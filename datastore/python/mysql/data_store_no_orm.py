"""
The data store for Python and MySQL (no ORM, DB-API 2.0, mysql-connector-python), used by the generated DAO.
"sdmc init -lang python -db mysql" writes it to the DAO folder, "sdmc ds" updates it to the sdmc version.

The generated DAO call only the adapter: the methods of DataStore below and OutParam. Keep their
names and parameters; everything else is yours: the connection, logging, retries, whatever you need.

    import mysql.connector
    from dbal.data_store_no_orm import create_ds

    ds = create_ds(mysql.connector.connect(host='localhost', database='mysql', user='root', password=''))
    tasks = TasksDao(ds).read_task_list()
    ds.commit()

Made from src/resources/data_store_no_orm.py of SQL DAL Maker: https://sqldalmaker.sourceforge.net/
"""


class OutParam:
    """
    OUT and INOUT parameters of a procedure: pass it among the parameters, read value after the call.
    """

    def __init__(self, value=None):
        self.value = value


class DataStore:

    def in_transaction(self) -> bool:
        pass

    def begin(self):
        pass

    def commit(self):
        pass

    def rollback(self):
        pass

    # the adapter of the generated DAO ------------------------------------------------------------------

    def insert_row(self, sql, params, ai_values):
        """
        :param sql: str
        :param params: list, values of SQL parameters
        :param ai_values: a list like [["o_id", None], ...] to obtain the generated values
        :return: None
        :raises Exception: if no rows inserted
        """
        pass

    def exec_dml(self, sql, params):
        """
        :param sql: str
        :param params: list, values of SQL parameters; OutParam for OUT and INOUT of a procedure, a list of callbacks for its result sets
        :return: int, the number of affected rows
        """
        pass

    def query_scalar(self, sql, params, out_params=None):
        """
        :param sql: str
        :param params: list, values of SQL parameters
        :param out_params: DAO methods declared with "out_params" pass it; not used here
        :return: the single scalar value
        :raises Exception: if the number of fetched rows != 1
        """
        pass

    def query_all_scalars(self, sql, params, out_params=None):
        """
        :param sql: str
        :param params: list, values of SQL parameters
        :param out_params: DAO methods declared with "out_params" pass it; not used here
        :return: list of scalar values
        """
        pass

    def query_row(self, sql, params, out_params=None):
        """
        :param sql: str
        :param params: list, values of SQL parameters
        :param out_params: DAO methods declared with "out_params" pass it; not used here
        :return: dict, the single fetched row, or an error string
        """
        pass

    def query_all_rows(self, sql, params, callback, out_params=None):
        """
        :param sql: str
        :param params: list, values of SQL parameters
        :param callback: called with each fetched row, a dict column -> value
        :param out_params: DAO methods declared with "out_params" pass it; not used here
        :return: None
        """
        pass


def create_ds(conn) -> DataStore:
    """
    :param conn: a DB-API connection of mysql-connector-python
    """
    return _DS(conn)


class _DS(DataStore):

    def __init__(self, conn):
        self.conn = conn

    def in_transaction(self) -> bool:
        return bool(self.conn.in_transaction)

    def begin(self):
        self.conn.start_transaction()

    def commit(self):
        self.conn.commit()

    def rollback(self):
        self.conn.rollback()

    def insert_row(self, sql, params, ai_values):
        sql = _format_sql(sql)

        def do_insert(cursor):
            cursor.execute(sql, params)
            if cursor.rowcount == 0:
                raise Exception('No rows inserted')
            if len(ai_values) > 0:
                ai_values[0][1] = cursor.lastrowid

        self._exec(do_insert)

    def exec_dml(self, sql, params):
        sql = _format_sql(sql)
        sp = _get_sp_name(sql)

        def do_exec(cursor):
            if sp is None:
                cursor.execute(sql, params)
                return cursor.rowcount
            result_args = cursor.callproc(sp, _get_call_params(params))
            # the result sets of the procedure go to the callbacks of the DAO, in their order
            for p in params:
                if isinstance(p, list) and len(p) > 0 and callable(p[0]):
                    for i, result in enumerate(cursor.stored_results()):
                        _fetch_all(result, p[i])
                    break
            _assign_out_params(params, result_args)
            return 0

        return self._exec(do_exec)

    def query_scalar(self, sql, params, out_params=None):
        rows = self.query_all_scalars(sql, params, out_params)
        if len(rows) == 0:
            raise Exception('No rows')
        if len(rows) > 1:
            raise Exception('More than 1 row exists')
        return rows[0]

    def query_all_scalars(self, sql, params, out_params=None):
        sql = _format_sql(sql)
        res = []
        sp = _get_sp_name(sql)

        def fetch_all(cursor):
            if sp is not None:
                result_args = cursor.callproc(sp, _get_call_params(params))
                for result in cursor.stored_results():
                    for row in result:
                        res.append(row[0])
                _assign_out_params(params, result_args)
                return
            cursor.execute(sql, params)
            for row in cursor:
                res.append(row[0])

        self._exec(fetch_all)
        return res

    def query_row(self, sql, params, out_params=None):
        rows = []
        self.query_all_rows(sql, params, lambda row: rows.append(row), out_params)
        if len(rows) == 1:
            return rows[0]
        if len(rows) == 0:
            return 'No rows'
        return 'More than 1 row exists'

    def query_all_rows(self, sql, params, callback, out_params=None):
        sql = _format_sql(sql)
        sp = _get_sp_name(sql)

        def fetch_all(cursor):
            if sp is not None:
                result_args = cursor.callproc(sp, _get_call_params(params))
                for result in cursor.stored_results():
                    _fetch_all(result, callback)
                _assign_out_params(params, result_args)
                return
            cursor.execute(sql, params)
            columns = [col[0] for col in cursor.description]
            for row in cursor:
                callback(dict(zip(columns, row)))

        self._exec(fetch_all)

    def _exec(self, func):
        cursor = self.conn.cursor()  # sqlite3 and mysql-connector cursors are not context managers
        try:
            return func(cursor)
        finally:
            cursor.close()


def _format_sql(sql):
    return sql.replace('?', '%s')


def _get_sp_name(sql):
    parts = sql.split()
    if len(parts) >= 2 and parts[0].strip().lower() == 'call':
        name = parts[1]
        end = name.find('(')
        if end == -1:
            return name
        return name[0:end]
    return None


def _get_call_params(params):
    """
    mysql-connector: the values of IN, OUT and INOUT parameters in their order; the callbacks of the
    result sets are not parameters.
    """
    call_params = []
    for p in params:
        if isinstance(p, OutParam):
            call_params.append(p.value)
        elif isinstance(p, list) and len(p) > 0 and callable(p[0]):
            pass
        else:
            call_params.append(p)
    return call_params


def _assign_out_params(params, result_args):
    i = 0
    for p in params:
        if isinstance(p, list) and len(p) > 0 and callable(p[0]):
            continue  # not in result_args
        if isinstance(p, OutParam):
            p.value = result_args[i]
        i += 1


def _fetch_all(result, callback):
    # a result set of a procedure: cursor(dictionary=True) does not apply to it
    columns = [d[0] for d in result.description]
    for row in result:
        callback(dict(zip(columns, row)))
