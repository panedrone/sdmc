"""
The data store for Python and SQL Server (no ORM, DB-API 2.0, mssql-python), used by the generated DAO.
"sdmc init -lang python -db mssql" writes it to the DAO folder, "sdmc ds" updates it to the sdmc version.

The generated DAO call only the adapter: the methods of DataStore below. Keep their
names and parameters; everything else is yours: the connection, logging, retries, whatever you need.

    import mssql_python
    from dbal.data_store_no_orm import create_ds

    ds = create_ds(mssql_python.connect('Server=localhost;Database=master;UID=sa;PWD=;Encrypt=no'))
    tasks = TasksDao(ds).read_task_list()
    ds.commit()

Made from src/resources/data_store_no_orm.py of SQL DAL Maker: https://sqldalmaker.sourceforge.net/
"""


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
        :param params: list, values of SQL parameters; a list of callbacks for the result sets of a procedure
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
    :param conn: a DB-API connection of mssql-python
    """
    return _DS(conn)


class _DS(DataStore):

    def __init__(self, conn):
        self.conn = conn

    def in_transaction(self) -> bool:
        # the ODBC driver keeps a transaction open all the time while autocommit is off
        return not self.conn.autocommit

    def begin(self):
        pass  # autocommit is off: there is always a transaction

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
                # no lastrowid in SQL Server: @@IDENTITY of the session, in a batch of its own
                cursor.execute('select cast(@@identity as bigint)')  # numeric(38, 0) otherwise
                ai_values[0][1] = cursor.fetchone()[0]

        self._exec(do_insert)

    def exec_dml(self, sql, params):
        sql = _format_sql(sql)
        callbacks = _get_callbacks(params)

        def do_exec(cursor):
            cursor.execute(sql, [p for p in params if not _is_callbacks(p)])
            if callbacks is None:
                return cursor.rowcount
            # the result sets of the procedure go to the callbacks of the DAO, in their order
            for callback in callbacks:
                _skip_counts(cursor)
                columns = [col[0] for col in cursor.description]
                for row in cursor.fetchall():
                    callback(dict(zip(columns, row)))
                cursor.nextset()
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

        def fetch_all(cursor):
            cursor.execute(sql, params)
            _skip_counts(cursor)
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

        def fetch_all(cursor):
            cursor.execute(sql, params)
            _skip_counts(cursor)
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
    # "call p(?, ?)" -> "{call p(?, ?)}", the ODBC escape; "{call ...}" and "exec ..." as is
    parts = sql.split(None, 1)
    if len(parts) == 2 and parts[0].lower() == 'call':
        return '{call ' + parts[1].strip() + '}'
    return sql


def _is_callbacks(p):
    return isinstance(p, list) and len(p) > 0 and callable(p[0])


def _get_callbacks(params):
    for p in params:
        if _is_callbacks(p):
            return p
    return None


def _skip_counts(cursor):
    # the row counts of a procedure without "set nocount on" come before its result set
    while cursor.description is None and cursor.nextset():
        pass
