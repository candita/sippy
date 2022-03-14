import { createTheme, makeStyles } from '@material-ui/core/styles'
import { DataGrid } from '@material-ui/data-grid'
import { Error } from '@material-ui/icons'
import { JsonParam, StringParam, useQueryParam } from 'use-query-params'
import Alert from '@material-ui/lab/Alert'
import GridToolbar from '../datagrid/GridToolbar'
import PropTypes from 'prop-types'
import React, { useEffect } from 'react'

const defaultTheme = createTheme()
const useStyles = makeStyles(
  (theme) => ({
    rowPhaseAccepted: {
      backgroundColor: theme.palette.success.light,
    },
    rowPhaseRejected: {
      backgroundColor: theme.palette.error.light,
    },
    title: {
      textAlign: 'center',
    },
  }),
  { defaultTheme }
)

function ReleasePayloadPullRequests(props) {
  const classes = useStyles()

  const columns = [
    {
      field: 'releaseTag',
      headerName: 'Tag',
      hide: true,
      flex: 1,
    },
    {
      field: 'pullRequestID',
      headerName: 'PR',
      flex: 0.5,
      renderCell: (params) => {
        return <a href={params.row.url}>{params.value}</a>
      },
    },
    {
      field: 'name',
      headerName: 'Repo',
      flex: 0.5,
      renderCell: (params) => {
        return <a href={params.row.url}>{params.value}</a>
      },
    },
    {
      field: 'description',
      headerName: 'Description',
      flex: 3,
      renderCell: (params) => {
        return <a href={params.row.url}>{params.value}</a>
      },
    },
  ]

  const [fetchError, setFetchError] = React.useState('')
  const [isLoaded, setLoaded] = React.useState(false)
  const [rows, setRows] = React.useState([])

  const [filterModel = props.filterModel, setFilterModel] = useQueryParam(
    'filters',
    JsonParam
  )

  const [sortField = props.sortField, setSortField] = useQueryParam(
    'sortField',
    StringParam
  )
  const [sort = props.sort, setSort] = useQueryParam('sort', StringParam)

  const requestSearch = (searchValue) => {
    const currentFilters = filterModel
    currentFilters.items = currentFilters.items.filter(
      (f) => f.columnField !== 'releaseTag'
    )
    currentFilters.items.push({
      id: 99,
      columnField: 'releaseTag',
      operatorValue: 'contains',
      value: searchValue,
    })
    setFilterModel(currentFilters)
  }

  const addFilters = (filter) => {
    const currentFilters = filterModel.items.filter((item) => item.value !== '')

    filter.forEach((item) => {
      if (item.value && item.value !== '') {
        currentFilters.push(item)
      }
    })
    setFilterModel({
      items: currentFilters,
      linkOperator: filterModel.linkOperator || 'and',
    })
  }

  const updateSortModel = (model) => {
    if (model.length === 0) {
      return
    }

    if (sort !== model[0].sort) {
      setSort(model[0].sort)
    }

    if (sortField !== model[0].field) {
      setSortField(model[0].field)
    }
  }

  const fetchData = () => {
    let queryString = ''
    if (filterModel && filterModel.items.length > 0) {
      queryString +=
        '&filter=' + encodeURIComponent(JSON.stringify(filterModel))
    }

    if (props.release && props.release !== '') {
      queryString += '&release=' + encodeURIComponent(props.release)
    }

    if (props.limit > 0) {
      queryString += '&limit=' + encodeURIComponent(props.limit)
    }

    queryString += '&sortField=' + encodeURIComponent(sortField)
    queryString += '&sort=' + encodeURIComponent(sort)

    fetch(
      process.env.REACT_APP_API_URL +
        '/api/releases/pullRequests?' +
        queryString.substring(1)
    )
      .then((response) => {
        if (response.status !== 200) {
          throw new Error('server returned ' + response.status)
        }
        return response.json()
      })
      .then((json) => {
        setRows(json)
        setLoaded(true)
      })
      .catch((error) => {
        setFetchError('Could not retrieve tags ' + error)
      })
  }

  useEffect(() => {
    fetchData()
  }, [filterModel])

  if (fetchError !== '') {
    return <Alert severity="error">{fetchError}</Alert>
  }

  if (isLoaded === false) {
    return <p>Loading...</p>
  }

  return (
    <DataGrid
      components={{ Toolbar: props.hideControls ? '' : GridToolbar }}
      rows={rows}
      columns={columns}
      autoHeight={true}
      disableColumnFilter={props.briefTable}
      disableColumnMenu={true}
      pageSize={props.pageSize}
      rowsPerPageOptions={[5, 10, 25, 50]}
      getRowClassName={(params) => classes['rowPhase' + params.row.phase]}
      filterMode="server"
      sortingMode="server"
      sortingOrder={['desc', 'asc']}
      sortModel={[
        {
          field: sortField,
          sort: sort,
        },
      ]}
      onSortModelChange={(m) => updateSortModel(m)}
      componentsProps={{
        toolbar: {
          columns: columns,
          clearSearch: () => requestSearch(''),
          doSearch: requestSearch,
          addFilters: addFilters,
          filterModel: filterModel,
          setFilterModel: setFilterModel,
        },
      }}
    />
  )
}

ReleasePayloadPullRequests.defaultProps = {
  limit: 0,
  hideControls: false,
  pageSize: 25,
  briefTable: false,
  filterModel: {
    items: [],
  },
  sortField: 'pullRequestID',
  sort: 'asc',
}

ReleasePayloadPullRequests.propTypes = {
  briefTable: PropTypes.bool,
  hideControls: PropTypes.bool,
  limit: PropTypes.number,
  pageSize: PropTypes.number,
  filterModel: PropTypes.object,
  release: PropTypes.string,
  sort: PropTypes.string,
  sortField: PropTypes.string,
}

export default ReleasePayloadPullRequests