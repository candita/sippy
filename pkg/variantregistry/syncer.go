package variantregistry

import (
	"context"
	"fmt"

	"cloud.google.com/go/bigquery"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"google.golang.org/api/iterator"
)

// Syncer is responsible for reconciling a given list of all known jobs and their variant key/values map to BigQuery.
type Syncer struct {
	bqClient        *bigquery.Client
	bigQueryProject string
	bigQueryDataSet string
	bigQueryTable   string
}

func NewSyncer(
	bigQueryClient *bigquery.Client,
	bigQueryProject string,
	bigQueryDataSet string,
	bigQueryTable string,
) *Syncer {

	return &Syncer{
		bqClient:        bigQueryClient,
		bigQueryProject: bigQueryProject,
		bigQueryDataSet: bigQueryDataSet,
		bigQueryTable:   bigQueryTable,
	}
}

// SyncJobVariants can be used to reconcile expected job variants with whatever is currently in the bigquery
// tables.
// If a job is missing from the current tables it will be added, of or if missing from expected it will be removed from
// the current tables.
// In the event a jobs variants have changed, they will be fully updated to match the new expected variants.
//
// The expectedVariants passed in is Sippy/Component Readiness deployment specific, users can define how they map
// job to variants, and then use this generic reconcile logic to get it into bigquery.
func (s *Syncer) SyncJobVariants(
	expectedVariants map[string]map[string]string) error {

	currentVariants, err := s.loadCurrentJobVariants()
	if err != nil {
		log.WithError(err).Error("error loading current job variants")
		return errors.Wrap(err, "error loading current job variants")
	}
	log.Infof("loaded %d current jobs with variants", len(currentVariants))

	inserts, updates, deletes, deleteJobs := compareVariants(expectedVariants, currentVariants)

	log.Infof("inserting %d new job variants", len(inserts))
	err = s.bulkInsertVariants(inserts)
	if err != nil {
		log.WithError(err).Error("error syncing job variants to bigquery")
	}

	log.Infof("updating %d job variants", len(updates))
	for i, jv := range updates {
		uLog := log.WithField("progress", fmt.Sprintf("%d/%d", i+1, len(updates)))
		err = s.updateVariant(uLog, jv)
		if err != nil {
			log.WithError(err).Error("error syncing job variants to bigquery")
		}
	}

	// This loop is called for variants being removed from a job that is still in the system.
	log.Infof("deleting %d job variants", len(deletes))
	for i, jv := range deletes {
		uLog := log.WithField("progress", fmt.Sprintf("%d/%d", i+1, len(deletes)))
		err = s.deleteVariant(uLog, jv)
		if err != nil {
			log.WithError(err).Error("error syncing job variants to bigquery")
		}
	}

	// Delete jobs entirely, much faster than one variant at a time when jobs have been removed.
	// This should be relatively rare and would require the job to not have run for weeks/months.
	log.Infof("deleting %d jobs", len(deleteJobs))
	for i, job := range deleteJobs {
		uLog := log.WithField("progress", fmt.Sprintf("%d/%d", i+1, len(updates)))
		err = s.deleteJob(uLog, job)
		if err != nil {
			log.WithError(err).Error("error syncing job variants to bigquery")
		}
	}

	return nil
}

// compareVariants compares the list of variants vs expected and returns the variants to be inserted, deleted, and updated.
// Broken out for unit testing purposes.
func compareVariants(expectedVariants, currentVariants map[string]map[string]string) (insertVariants, updateVariants, deleteVariants []jobVariant, deleteJobs []string) {
	insertVariants = []jobVariant{}
	updateVariants = []jobVariant{}
	deleteVariants = []jobVariant{}
	deleteJobs = []string{}

	for expectedJob, expectedVariants := range expectedVariants {
		if _, ok := currentVariants[expectedJob]; !ok {
			// Handle net new jobs:
			for k, v := range expectedVariants {
				insertVariants = append(insertVariants, jobVariant{
					JobName:      expectedJob,
					VariantName:  k,
					VariantValue: v,
				})
			}
			continue
		}

		// Sync variants for an existing job if any have changed:
		for k, v := range expectedVariants {
			currVarVal, ok := currentVariants[expectedJob][k]
			if !ok {
				// New variant added:
				insertVariants = append(insertVariants, jobVariant{
					JobName:      expectedJob,
					VariantName:  k,
					VariantValue: v,
				})
			} else {
				if currVarVal != v {
					updateVariants = append(updateVariants, jobVariant{
						JobName:      expectedJob,
						VariantName:  k,
						VariantValue: v,
					})
				}
			}
		}

		// Look for any variants for this job that should be removed:
		for k, v := range currentVariants[expectedJob] {
			if _, ok := expectedVariants[k]; !ok {
				deleteVariants = append(deleteVariants, jobVariant{
					JobName:      expectedJob,
					VariantName:  k,
					VariantValue: v,
				})
			}
		}
	}

	// Look for any jobs that should be removed:
	for currJobName := range currentVariants {
		if _, ok := expectedVariants[currJobName]; !ok {
			deleteJobs = append(deleteJobs, currJobName)
		}
	}

	return insertVariants, updateVariants, deleteVariants, deleteJobs
}

func (s *Syncer) loadCurrentJobVariants() (map[string]map[string]string, error) {
	query := s.bqClient.Query(`SELECT * FROM ` +
		fmt.Sprintf("%s.%s.%s", s.bigQueryProject, s.bigQueryDataSet, s.bigQueryTable) +
		` ORDER BY job_name, variant_name`)
	it, err := query.Read(context.TODO())
	if err != nil {
		return nil, errors.Wrap(err, "error querying current job variants")
	}

	currentVariants := map[string]map[string]string{}

	for {
		jv := jobVariant{}
		err := it.Next(&jv)
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.WithError(err).Error("error parsing job variant from bigquery")
			return nil, err
		}
		if _, ok := currentVariants[jv.JobName]; !ok {
			currentVariants[jv.JobName] = map[string]string{}
		}
		currentVariants[jv.JobName][jv.VariantName] = jv.VariantValue
	}

	return currentVariants, nil
}

type jobVariant struct {
	JobName      string `bigquery:"job_name"`
	VariantName  string `bigquery:"variant_name"`
	VariantValue string `bigquery:"variant_value"`
}

// bulkInsertVariants inserts all new job variants in batches.
func (s *Syncer) bulkInsertVariants(inserts []jobVariant) error {
	var batchSize = 500

	table := s.bqClient.Dataset(s.bigQueryDataSet).Table(s.bigQueryTable)
	inserter := table.Inserter()
	for i := 0; i < len(inserts); i += batchSize {
		end := i + batchSize
		if end > len(inserts) {
			end = len(inserts)
		}

		if err := inserter.Put(context.TODO(), inserts[i:end]); err != nil {
			return err
		}
		log.Infof("added %d new job variant rows", end-i)
	}

	return nil
}

// updateVariant updates a job variant in the registry.
func (s *Syncer) updateVariant(logger log.FieldLogger, jv jobVariant) error {
	queryStr := fmt.Sprintf("UPDATE `%s.%s.%s` SET variant_value = '%s' WHERE job_name = '%s' and variant_name = '%s'",
		s.bigQueryProject, s.bigQueryDataSet, s.bigQueryTable, jv.VariantValue, jv.JobName, jv.VariantName)
	insertQuery := s.bqClient.Query(queryStr)
	_, err := insertQuery.Read(context.TODO())
	if err != nil {
		return errors.Wrapf(err, "error updating variants: %s", queryStr)
	}
	logger.Infof("successful query: %s", queryStr)
	return nil
}

// deleteVariant deletes a job variant in the registry.
func (s *Syncer) deleteVariant(logger log.FieldLogger, jv jobVariant) error {
	queryStr := fmt.Sprintf("DELETE FROM `%s.%s.%s` WHERE job_name = '%s' and variant_name = '%s' and variant_value = '%s'",
		s.bigQueryProject, s.bigQueryDataSet, s.bigQueryTable, jv.JobName, jv.VariantName, jv.VariantValue)
	insertQuery := s.bqClient.Query(queryStr)
	_, err := insertQuery.Read(context.TODO())
	if err != nil {
		return errors.Wrapf(err, "error deleting variant: %s", queryStr)
	}
	logger.Infof("successful query: %s", queryStr)
	return nil
}

// deleteJob deletes all variants for a given job in the registry.
func (s *Syncer) deleteJob(logger log.FieldLogger, job string) error {
	queryStr := fmt.Sprintf("DELETE FROM `%s.%s.%s` WHERE job_name = '%s'",
		s.bigQueryProject, s.bigQueryDataSet, s.bigQueryTable, job)
	insertQuery := s.bqClient.Query(queryStr)
	_, err := insertQuery.Read(context.TODO())
	if err != nil {
		return errors.Wrapf(err, "error deleting job: %s", queryStr)
	}
	logger.Infof("successful query: %s", queryStr)
	return nil
}
