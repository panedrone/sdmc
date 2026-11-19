#ifndef SDM_DATA_STORE_H
#define SDM_DATA_STORE_H

/*
    The data store for C++, Qt 5/6 and SQLite (QtSql, the driver QSQLITE), used by the generated DAO.
    "sdmc init -lang cpp -db sqlite" writes it next to the DAO, "sdmc ds" updates it to the sdmc version.

    The generated DAO use only the adapter: CRef, CValue and the value classes, CRowProcessor,
    CListReader, DL_STR and DL_T, the methods of CDataStore under "the adapter of the generated DAO".
    Keep their names and parameters; everything else is yours: the connection, transactions, logging,
    more value classes for the type map of settings.xml.

        CDataStore ds;
        if (!ds.open("app.sqlite3")) {
            qDebug() << ds.getLastErrors();
        }
        TasksDao dao(ds);

    Made from DataStore_Qt5.h of SQL DAL Maker: https://sqldalmaker.sourceforge.net/
 */

#include <QtSql/QSqlDatabase>
#include <QByteArray>
#include <QDate>
#include <QDateTime>
#include <QString>
#include <QTime>
#include <QVariant>

#include <list>
#include <type_traits>

// nullOf is the NULL of type T: a typed one, so that any driver knows what it binds
template <class T>
QVariant nullOf() {
#if QT_VERSION >= QT_VERSION_CHECK(6, 0, 0)
    return QVariant(QMetaType::fromType<T>());
#else
    return QVariant(qMetaTypeId<T>(), nullptr);
#endif
}

/*
    A field of a model or a parameter of a DAO method is one of two kinds:
      - a NOT NULL column is the value itself: qint32, double, QString, QByteArray, QDateTime, ... NULL is
        never sent (QString() goes as ""), a NULL that comes anyway is 0 or empty;
      - a nullable column is a value class from CNullable<T> (CInt32N, CDoubleN, CTextN, CBlobN, ...):
        Value and IsNull; a new one is NULL, set IsNull = false along with Value.
    Which of the two a field gets is up to the type map of settings.xml:
    target="${nullable}|0:QString|1:CTextN", where the macro ${nullable} picks 1 for a nullable column
    of a table and 0 otherwise.

    CValue is a value class without data of its own: QtSql passes values as QVariant, and each value
    class converts its Value to and from it, NULL included: toVariant() for a parameter, fromVariant()
    for a fetched column. A class of your own is a typedef of CNullable<T> (T is any type QVariant
    converts: typedef CNullable<QUuid> CUuidN) or, for anything else, a Value and the two conversions.
    CVariant is one: QVariant has its own NULL.

    CRef is what the generated DAO pass to CDataStore: a reference to a field or a parameter of either
    kind, a value or a CValue.
 */
class CValue {
public:

    virtual ~CValue() {
    }

    virtual QVariant toVariant() const = 0;

    virtual void fromVariant(const QVariant &v) = 0;
};

// CVariant holds anything: the type of the fields the type map has no class for (<type-map default="CVariant">).
class CVariant : public CValue {
public:

    QVariant Value; // QVariant() is NULL

    QVariant toVariant() const override {
        return Value;
    }

    void fromVariant(const QVariant &v) override {
        Value = v;
    }
};

// notNull is the value of a NOT NULL column: QString() and QByteArray() go as "", never as NULL
template <class T>
QVariant notNull(const T &v) {
    return QVariant::fromValue(v);
}

inline QVariant notNull(const QString &v) {
    return v.isNull() ? QVariant(QString("")) : QVariant(v);
}

inline QVariant notNull(const QByteArray &v) {
    return v.isNull() ? QVariant(QByteArray("")) : QVariant(v);
}

// CNullable is a nullable column: Value and IsNull. A new one is NULL; set IsNull = false along with Value.
template <class T>
class CNullable : public CValue {
public:

    CNullable() : Value(), IsNull(true) {
    }

    T Value;
    bool IsNull;

    QVariant toVariant() const override {
        return IsNull ? nullOf<T>() : notNull(Value);
    }

    void fromVariant(const QVariant &v) override {
        IsNull = v.isNull();
        Value = IsNull ? T() : v.value<T>();
    }
};

// the classes of nullable columns; one more type is one more line. No 8-bit integers: qint8 and quint8
// are chars in C++, a stream prints them as characters; TINYINT goes to qint16 or quint16.
typedef CNullable<qint16> CInt16N;
typedef CNullable<qint32> CInt32N;
typedef CNullable<qint64> CInt64N;
typedef CNullable<quint16> CUInt16N;
typedef CNullable<quint32> CUInt32N;
typedef CNullable<quint64> CUInt64N;
typedef CNullable<float> CFloatN;
typedef CNullable<double> CDoubleN;
typedef CNullable<QString> CTextN;
typedef CNullable<QByteArray> CBlobN;
typedef CNullable<bool> CBoolN;
typedef CNullable<QDate> CDateN;
typedef CNullable<QTime> CTimeN;
typedef CNullable<QDateTime> CDateTimeN;

/*
    CRef refers to a field or a parameter for CDataStore: a value of a NOT NULL column (qint32 &, QString &)
    or a CValue. The generated DAO make arrays of them: CRef params[] = {p.t_subject, p.t_comment};
    It takes a const reference for both: parameters are only read, fetched values are written to the
    fields of a model or to res, which are never const.
 */
class CRef {
public:

    CRef(const CValue &v) : ptr(const_cast<CValue *>(&v)), to(&valueTo), from(&valueFrom) {
    }

    template <class T, class = typename std::enable_if<!std::is_base_of<CValue, T>::value>::type>
    CRef(const T &v) : ptr(const_cast<T *>(&v)), to(&rawTo<T>), from(&rawFrom<T>) {
    }

    QVariant toVariant() const {
        return to(ptr);
    }

    void fromVariant(const QVariant &v) const {
        from(ptr, v);
    }

private:

    void *ptr;
    QVariant (*to)(const void *);
    void (*from)(void *, const QVariant &);

    static QVariant valueTo(const void *p) {
        return static_cast<const CValue *>(p)->toVariant();
    }

    static void valueFrom(void *p, const QVariant &v) {
        static_cast<CValue *>(p)->fromVariant(v);
    }

    // a NULL that comes to a NOT NULL value is T(): 0 or empty
    template <class T>
    static QVariant rawTo(const void *p) {
        return notNull(*static_cast<const T *>(p));
    }

    template <class T>
    static void rawFrom(void *p, const QVariant &v) {
        *static_cast<T *>(p) = v.value<T>();
    }
};

class CRowProcessor {
public:

    virtual ~CRowProcessor() {
    }

    virtual void processRow() = 0;
};

/*
    A DAO method for a list calls read() for each fetched row:

        struct : CListReader<Task> {
            void read(Task &t) { qDebug() << t.t_subject; }
        } reader;
        dao.read_task_list(reader);
 */
template <class T>
class CListReader {
public:

    virtual ~CListReader() {
    }

    virtual void read(T &t) = 0;
};

/*
    CListReader2 collects the rows:

        CListReader2<Task> reader;
        dao.read_task_list(reader);
        qDebug() << reader.list.size();
 */
template <class T>
class CListReader2 : public CListReader<T> {
public:

    std::list<T> list;

    void read(T &t) {
        list.push_back(t);
    }
};

#define DL_STR const char *
#define DL_T(quote) quote

class QSqlQuery;

class CDataStore {
public:

    // connectionName: the QSqlDatabase connection of this data store
    explicit CDataStore(const QString &connectionName = QLatin1String(QSqlDatabase::defaultConnection));

    // the errors of the last call, empty if it succeeded
    const QString &getLastErrors() const {
        return errors;
    }

    // the SQLite file; yours, as everything but the adapter
    bool open(const QString &file);
    void close();

    bool begin();
    bool commit();
    bool rollback();
    bool inTransaction() const {
        return transaction;
    }

    // the adapter of the generated DAO -------------------------------------------------------------

    // AI_values: the generated values, at most one; without them the DAO call execDML instead.
    // Returns the number of inserted rows or -1 on error.
    int insert(DL_STR sql, int params_count, const CRef params[], int AI_values_count, const CRef AI_values[]);

    // Returns the number of affected rows or -1 on error.
    int execDML(DL_STR sql, int params_count, const CRef params[]);

    // No params: params_count == 0 and params == 0. Returns the number of fetched rows or -1 on error;
    // more than 1 row is an error too (getLastErrors), ret_values have the last one.
    int queryRow(DL_STR sql, int params_count, const CRef params[], int ret_values_count, const CRef ret_values[], DL_STR col_names[]);

    // No params: params_count == 0 and params == 0. Returns the number of fetched rows or -1 on error.
    int queryRowList(DL_STR sql, int params_count, const CRef params[], int ret_values_count, const CRef ret_values[], DL_STR col_names[], CRowProcessor *rowProcessor);

private:

    QString connectionName;
    QString errors;
    bool transaction;

    bool exec(DL_STR sql, QSqlQuery &query, int params_count, const CRef params[]);
    bool readRow(QSqlQuery &query, int ret_values_count, const CRef ret_values[], DL_STR col_names[]);
};

#endif // SDM_DATA_STORE_H
