package mssql

import (
	"errors"
	"net/url"
	"strings"
)

// ConnString returns the connection string of go-mssqldb for <db url user pwd>. Both forms are accepted:
//   - go-mssqldb: sqlserver://host:1433?database=Northwind (instance: sqlserver://host/SQLEXPRESS);
//   - JDBC (with or without "jdbc:"): sqlserver://host:1433;databaseName=Northwind;encrypt=false
//     (instance: host\SQLEXPRESS or instanceName=SQLEXPRESS; integratedSecurity=true: Windows login).
//
// Credentials in the URL take precedence over user and pwd.
func ConnString(dbURL, user, pwd string) (string, error) {
	raw := strings.TrimPrefix(strings.TrimSpace(dbURL), "jdbc:")
	rest, ok := strings.CutPrefix(raw, "sqlserver://")
	if !ok {
		return "", errors.New("SQL Server URL expected (sqlserver://host:1433?database=db or jdbc:sqlserver://host:1433;databaseName=db): " + dbURL)
	}
	if !strings.Contains(rest, ";") {
		u, err := url.Parse(raw)
		if err != nil {
			return "", err
		}
		if u.User == nil && user != "" {
			u.User = url.UserPassword(user, pwd)
		}
		return u.String(), nil
	}
	// JDBC: host[\instance][:port];key=value;...
	parts := strings.Split(rest, ";")
	host, instance := parts[0], ""
	if i := strings.IndexByte(host, '\\'); i >= 0 {
		host, instance = host[:i], host[i+1:]
		if j := strings.IndexByte(instance, ':'); j >= 0 { // host\instance:port
			host += instance[j:]
			instance = instance[:j]
		}
	}
	q := url.Values{}
	integrated := false
	for _, p := range parts[1:] {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "databasename", "database":
			q.Set("database", v)
		case "encrypt":
			q.Set("encrypt", v)
		case "trustservercertificate":
			q.Set("TrustServerCertificate", v)
		case "instancename":
			instance = v
		case "integratedsecurity":
			integrated = strings.EqualFold(v, "true")
		case "user":
			user = v
		case "password":
			pwd = v
		}
	}
	// mssql-jdbc with encrypt=false encrypts the login without validating the certificate; go-mssqldb validates it
	if enc := strings.ToLower(q.Get("encrypt")); (enc == "false" || enc == "no") && q.Get("TrustServerCertificate") == "" {
		q.Set("TrustServerCertificate", "true")
	}
	u := url.URL{Scheme: "sqlserver", Host: host, RawQuery: q.Encode()}
	if instance != "" {
		u.Path = "/" + instance
	}
	if user != "" && !integrated {
		u.User = url.UserPassword(user, pwd)
	}
	return u.String(), nil
}
