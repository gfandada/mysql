// Go MySQL Driver - A MySQL-Driver for Go's database/sql package
//
// Copyright 2017 The Go-MySQL-Driver Authors. All rights reserved.
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this file,
// You can obtain one at http://mozilla.org/MPL/2.0/.

// +build go1.8

package mysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// static interface implementation checks of mysqlConn
var (
	_ driver.ConnBeginTx        = &mysqlConn{}
	_ driver.ConnPrepareContext = &mysqlConn{}
	_ driver.ExecerContext      = &mysqlConn{}
	_ driver.Pinger             = &mysqlConn{}
	_ driver.QueryerContext     = &mysqlConn{}
)

// static interface implementation checks of mysqlStmt
var (
	_ driver.StmtExecContext  = &mysqlStmt{}
	_ driver.StmtQueryContext = &mysqlStmt{}
)

// Ensure that all the driver interfaces are implemented
var (
	_ driver.RowsColumnTypeDatabaseTypeName = &binaryRows{}
	_ driver.RowsColumnTypeDatabaseTypeName = &textRows{}
	_ driver.RowsColumnTypeLength           = &binaryRows{}
	_ driver.RowsColumnTypeLength           = &textRows{}
	_ driver.RowsColumnTypeNullable         = &binaryRows{}
	_ driver.RowsColumnTypeNullable         = &textRows{}
	_ driver.RowsColumnTypePrecisionScale   = &binaryRows{}
	_ driver.RowsColumnTypePrecisionScale   = &textRows{}
	_ driver.RowsColumnTypeScanType         = &binaryRows{}
	_ driver.RowsColumnTypeScanType         = &textRows{}
	_ driver.RowsNextResultSet              = &binaryRows{}
	_ driver.RowsNextResultSet              = &textRows{}
)

func TestMultiResultSet(t *testing.T) {
	type result struct {
		values  [][]int
		columns []string
	}

	// checkRows is a helper test function to validate rows containing 3 result
	// sets with specific values and columns. The basic query would look like this:
	//
	// SELECT 1 AS col1, 2 AS col2 UNION SELECT 3, 4;
	// SELECT 0 UNION SELECT 1;
	// SELECT 1 AS col1, 2 AS col2, 3 AS col3 UNION SELECT 4, 5, 6;
	//
	// to distinguish test cases the first string argument is put in front of
	// every error or fatal message.
	checkRows := func(desc string, rows *sql.Rows, dbt *DBTest) {
		expected := []result{
			{
				values:  [][]int{{1, 2}, {3, 4}},
				columns: []string{"col1", "col2"},
			},
			{
				values:  [][]int{{1, 2, 3}, {4, 5, 6}},
				columns: []string{"col1", "col2", "col3"},
			},
		}

		var res1 result
		for rows.Next() {
			var res [2]int
			if err := rows.Scan(&res[0], &res[1]); err != nil {
				dbt.Fatal(err)
			}
			res1.values = append(res1.values, res[:])
		}

		cols, err := rows.Columns()
		if err != nil {
			dbt.Fatal(desc, err)
		}
		res1.columns = cols

		if !reflect.DeepEqual(expected[0], res1) {
			dbt.Error(desc, "want =", expected[0], "got =", res1)
		}

		if !rows.NextResultSet() {
			dbt.Fatal(desc, "expected next result set")
		}

		// ignoring one result set

		if !rows.NextResultSet() {
			dbt.Fatal(desc, "expected next result set")
		}

		var res2 result
		cols, err = rows.Columns()
		if err != nil {
			dbt.Fatal(desc, err)
		}
		res2.columns = cols

		for rows.Next() {
			var res [3]int
			if err := rows.Scan(&res[0], &res[1], &res[2]); err != nil {
				dbt.Fatal(desc, err)
			}
			res2.values = append(res2.values, res[:])
		}

		if !reflect.DeepEqual(expected[1], res2) {
			dbt.Error(desc, "want =", expected[1], "got =", res2)
		}

		if rows.NextResultSet() {
			dbt.Error(desc, "unexpected next result set")
		}

		if err := rows.Err(); err != nil {
			dbt.Error(desc, err)
		}
	}

	runTestsWithMultiStatement(t, dsn, func(dbt *DBTest) {
		rows := dbt.mustQuery(`DO 1;
		SELECT 1 AS col1, 2 AS col2 UNION SELECT 3, 4;
		DO 1;
		SELECT 0 UNION SELECT 1;
		SELECT 1 AS col1, 2 AS col2, 3 AS col3 UNION SELECT 4, 5, 6;`)
		defer rows.Close()
		checkRows("query: ", rows, dbt)
	})

	runTestsWithMultiStatement(t, dsn, func(dbt *DBTest) {
		queries := []string{
			`
			DROP PROCEDURE IF EXISTS test_mrss;
			CREATE PROCEDURE test_mrss()
			BEGIN
				DO 1;
				SELECT 1 AS col1, 2 AS col2 UNION SELECT 3, 4;
				DO 1;
				SELECT 0 UNION SELECT 1;
				SELECT 1 AS col1, 2 AS col2, 3 AS col3 UNION SELECT 4, 5, 6;
			END
		`,
			`
			DROP PROCEDURE IF EXISTS test_mrss;
			CREATE PROCEDURE test_mrss()
			BEGIN
				SELECT 1 AS col1, 2 AS col2 UNION SELECT 3, 4;
				SELECT 0 UNION SELECT 1;
				SELECT 1 AS col1, 2 AS col2, 3 AS col3 UNION SELECT 4, 5, 6;
			END
		`,
		}

		defer dbt.mustExec("DROP PROCEDURE IF EXISTS test_mrss")

		for i, query := range queries {
			dbt.mustExec(query)

			stmt, err := dbt.db.Prepare("CALL test_mrss()")
			if err != nil {
				dbt.Fatalf("%v (i=%d)", err, i)
			}
			defer stmt.Close()

			for j := 0; j < 2; j++ {
				rows, err := stmt.Query()
				if err != nil {
					dbt.Fatalf("%v (i=%d) (j=%d)", err, i, j)
				}
				checkRows(fmt.Sprintf("prepared stmt query (i=%d) (j=%d): ", i, j), rows, dbt)
			}
		}
	})
}

func TestMultiResultSetNoSelect(t *testing.T) {
	runTestsWithMultiStatement(t, dsn, func(dbt *DBTest) {
		rows := dbt.mustQuery("DO 1; DO 2;")
		defer rows.Close()

		if rows.Next() {
			dbt.Error("unexpected row")
		}

		if rows.NextResultSet() {
			dbt.Error("unexpected next result set")
		}

		if err := rows.Err(); err != nil {
			dbt.Error("expected nil; got ", err)
		}
	})
}

// tests if rows are set in a proper state if some results were ignored before
// calling rows.NextResultSet.
func TestSkipResults(t *testing.T) {
	runTests(t, dsn, func(dbt *DBTest) {
		rows := dbt.mustQuery("SELECT 1, 2")
		defer rows.Close()

		if !rows.Next() {
			dbt.Error("expected row")
		}

		if rows.NextResultSet() {
			dbt.Error("unexpected next result set")
		}

		if err := rows.Err(); err != nil {
			dbt.Error("expected nil; got ", err)
		}
	})
}

func TestPingContext(t *testing.T) {
	runTests(t, dsn, func(dbt *DBTest) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := dbt.db.PingContext(ctx); err != context.Canceled {
			dbt.Errorf("expected context.Canceled, got %v", err)
		}
	})
}

func TestContextCancelExec(t *testing.T) {
	runTests(t, dsn, func(dbt *DBTest) {
		dbt.mustExec("CREATE TABLE test (v INTEGER)")
		ctx, cancel := context.WithCancel(context.Background())

		// Delay execution for just a bit until db.ExecContext has begun.
		defer time.AfterFunc(100*time.Millisecond, cancel).Stop()

		// This query will be canceled.
		startTime := time.Now()
		if _, err := dbt.db.ExecContext(ctx, "INSERT INTO test VALUES (SLEEP(1))"); err != context.Canceled {
			dbt.Errorf("expected context.Canceled, got %v", err)
		}
		if d := time.Since(startTime); d > 500*time.Millisecond {
			dbt.Errorf("too long execution time: %s", d)
		}

		// Wait for the INSERT query has done.
		time.Sleep(time.Second)

		// Check how many times the query is executed.
		var v int
		if err := dbt.db.QueryRow("SELECT COUNT(*) FROM test").Scan(&v); err != nil {
			dbt.Fatalf("%s", err.Error())
		}
		if v != 1 { // TODO: need to kill the query, and v should be 0.
			dbt.Errorf("expected val to be 1, got %d", v)
		}

		// Context is already canceled, so error should come before execution.
		if _, err := dbt.db.ExecContext(ctx, "INSERT INTO test VALUES (1)"); err == nil {
			dbt.Error("expected error")
		} else if err.Error() != "context canceled" {
			dbt.Fatalf("unexpected error: %s", err)
		}

		// The second insert query will fail, so the table has no changes.
		if err := dbt.db.QueryRow("SELECT COUNT(*) FROM test").Scan(&v); err != nil {
			dbt.Fatalf("%s", err.Error())
		}
		if v != 1 {
			dbt.Errorf("expected val to be 1, got %d", v)
		}
	})
}

func TestContextCancelQuery(t *testing.T) {
	runTests(t, dsn, func(dbt *DBTest) {
		dbt.mustExec("CREATE TABLE test (v INTEGER)")
		ctx, cancel := context.WithCancel(context.Background())

		// Delay execution for just a bit until db.ExecContext has begun.
		defer time.AfterFunc(100*time.Millisecond, cancel).Stop()

		// This query will be canceled.
		startTime := time.Now()
		if _, err := dbt.db.QueryContext(ctx, "INSERT INTO test VALUES (SLEEP(1))"); err != context.Canceled {
			dbt.Errorf("expected context.Canceled, got %v", err)
		}
		if d := time.Since(startTime); d > 500*time.Millisecond {
			dbt.Errorf("too long execution time: %s", d)
		}

		// Wait for the INSERT query has done.
		time.Sleep(time.Second)

		// Check how many times the query is executed.
		var v int
		if err := dbt.db.QueryRow("SELECT COUNT(*) FROM test").Scan(&v); err != nil {
			dbt.Fatalf("%s", err.Error())
		}
		if v != 1 { // TODO: need to kill the query, and v should be 0.
			dbt.Errorf("expected val to be 1, got %d", v)
		}

		// Context is already canceled, so error should come before execution.
		if _, err := dbt.db.QueryContext(ctx, "INSERT INTO test VALUES (1)"); err != context.Canceled {
			dbt.Errorf("expected context.Canceled, got %v", err)
		}

		// The second insert query will fail, so the table has no changes.
		if err := dbt.db.QueryRow("SELECT COUNT(*) FROM test").Scan(&v); err != nil {
			dbt.Fatalf("%s", err.Error())
		}
		if v != 1 {
			dbt.Errorf("expected val to be 1, got %d", v)
		}
	})
}

func TestContextCancelQueryRow(t *testing.T) {
	runTests(t, dsn, func(dbt *DBTest) {
		dbt.mustExec("CREATE TABLE test (v INTEGER)")
		dbt.mustExec("INSERT INTO test VALUES (1), (2), (3)")
		ctx, cancel := context.WithCancel(context.Background())

		rows, err := dbt.db.QueryContext(ctx, "SELECT v FROM test")
		if err != nil {
			dbt.Fatalf("%s", err.Error())
		}

		// the first row will be succeed.
		var v int
		if !rows.Next() {
			dbt.Fatalf("unexpected end")
		}
		if err := rows.Scan(&v); err != nil {
			dbt.Fatalf("%s", err.Error())
		}

		cancel()
		// make sure the driver recieve cancel request.
		time.Sleep(100 * time.Millisecond)

		if rows.Next() {
			dbt.Errorf("expected end, but not")
		}
		if err := rows.Err(); err != context.Canceled {
			dbt.Errorf("expected context.Canceled, got %v", err)
		}
	})
}

func TestContextCancelPrepare(t *testing.T) {
	runTests(t, dsn, func(dbt *DBTest) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := dbt.db.PrepareContext(ctx, "SELECT 1"); err != context.Canceled {
			dbt.Errorf("expected context.Canceled, got %v", err)
		}
	})
}

func TestContextCancelStmtExec(t *testing.T) {
	runTests(t, dsn, func(dbt *DBTest) {
		dbt.mustExec("CREATE TABLE test (v INTEGER)")
		ctx, cancel := context.WithCancel(context.Background())
		stmt, err := dbt.db.PrepareContext(ctx, "INSERT INTO test VALUES (SLEEP(1))")
		if err != nil {
			dbt.Fatalf("unexpected error: %v", err)
		}

		// Delay execution for just a bit until db.ExecContext has begun.
		defer time.AfterFunc(100*time.Millisecond, cancel).Stop()

		// This query will be canceled.
		startTime := time.Now()
		if _, err := stmt.ExecContext(ctx); err != context.Canceled {
			dbt.Errorf("expected context.Canceled, got %v", err)
		}
		if d := time.Since(startTime); d > 500*time.Millisecond {
			dbt.Errorf("too long execution time: %s", d)
		}

		// Wait for the INSERT query has done.
		time.Sleep(time.Second)

		// Check how many times the query is executed.
		var v int
		if err := dbt.db.QueryRow("SELECT COUNT(*) FROM test").Scan(&v); err != nil {
			dbt.Fatalf("%s", err.Error())
		}
		if v != 1 { // TODO: need to kill the query, and v should be 0.
			dbt.Errorf("expected val to be 1, got %d", v)
		}
	})
}

func TestContextCancelStmtQuery(t *testing.T) {
	runTests(t, dsn, func(dbt *DBTest) {
		dbt.mustExec("CREATE TABLE test (v INTEGER)")
		ctx, cancel := context.WithCancel(context.Background())
		stmt, err := dbt.db.PrepareContext(ctx, "INSERT INTO test VALUES (SLEEP(1))")
		if err != nil {
			dbt.Fatalf("unexpected error: %v", err)
		}

		// Delay execution for just a bit until db.ExecContext has begun.
		defer time.AfterFunc(100*time.Millisecond, cancel).Stop()

		// This query will be canceled.
		startTime := time.Now()
		if _, err := stmt.QueryContext(ctx); err != context.Canceled {
			dbt.Errorf("expected context.Canceled, got %v", err)
		}
		if d := time.Since(startTime); d > 500*time.Millisecond {
			dbt.Errorf("too long execution time: %s", d)
		}

		// Wait for the INSERT query has done.
		time.Sleep(time.Second)

		// Check how many times the query is executed.
		var v int
		if err := dbt.db.QueryRow("SELECT COUNT(*) FROM test").Scan(&v); err != nil {
			dbt.Fatalf("%s", err.Error())
		}
		if v != 1 { // TODO: need to kill the query, and v should be 0.
			dbt.Errorf("expected val to be 1, got %d", v)
		}
	})
}

func TestContextCancelBegin(t *testing.T) {
	runTests(t, dsn, func(dbt *DBTest) {
		dbt.mustExec("CREATE TABLE test (v INTEGER)")
		ctx, cancel := context.WithCancel(context.Background())
		tx, err := dbt.db.BeginTx(ctx, nil)
		if err != nil {
			dbt.Fatal(err)
		}

		// Delay execution for just a bit until db.ExecContext has begun.
		defer time.AfterFunc(100*time.Millisecond, cancel).Stop()

		// This query will be canceled.
		startTime := time.Now()
		if _, err := tx.ExecContext(ctx, "INSERT INTO test VALUES (SLEEP(1))"); err != context.Canceled {
			dbt.Errorf("expected context.Canceled, got %v", err)
		}
		if d := time.Since(startTime); d > 500*time.Millisecond {
			dbt.Errorf("too long execution time: %s", d)
		}

		// Transaction is canceled, so expect an error.
		switch err := tx.Commit(); err {
		case sql.ErrTxDone:
			// because the transaction has already been rollbacked.
			// the database/sql package watches ctx
			// and rollbacks when ctx is canceled.
		case context.Canceled:
			// the database/sql package rollbacks on another goroutine,
			// so the transaction may not be rollbacked depending on goroutine scheduling.
		default:
			dbt.Errorf("expected sql.ErrTxDone or context.Canceled, got %v", err)
		}

		// Context is canceled, so cannot begin a transaction.
		if _, err := dbt.db.BeginTx(ctx, nil); err != context.Canceled {
			dbt.Errorf("expected context.Canceled, got %v", err)
		}
	})
}

func TestContextBeginIsolationLevel(t *testing.T) {
	runTests(t, dsn, func(dbt *DBTest) {
		dbt.mustExec("CREATE TABLE test (v INTEGER)")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		tx1, err := dbt.db.BeginTx(ctx, &sql.TxOptions{
			Isolation: sql.LevelRepeatableRead,
		})
		if err != nil {
			dbt.Fatal(err)
		}

		tx2, err := dbt.db.BeginTx(ctx, &sql.TxOptions{
			Isolation: sql.LevelReadCommitted,
		})
		if err != nil {
			dbt.Fatal(err)
		}

		_, err = tx1.ExecContext(ctx, "INSERT INTO test VALUES (1)")
		if err != nil {
			dbt.Fatal(err)
		}

		var v int
		row := tx2.QueryRowContext(ctx, "SELECT COUNT(*) FROM test")
		if err := row.Scan(&v); err != nil {
			dbt.Fatal(err)
		}
		// Because writer transaction wasn't commited yet, it should be available
		if v != 0 {
			dbt.Errorf("expected val to be 0, got %d", v)
		}

		err = tx1.Commit()
		if err != nil {
			dbt.Fatal(err)
		}

		row = tx2.QueryRowContext(ctx, "SELECT COUNT(*) FROM test")
		if err := row.Scan(&v); err != nil {
			dbt.Fatal(err)
		}
		// Data written by writer transaction is already commited, it should be selectable
		if v != 1 {
			dbt.Errorf("expected val to be 1, got %d", v)
		}
		tx2.Commit()
	})
}

func TestContextBeginReadOnly(t *testing.T) {
	runTests(t, dsn, func(dbt *DBTest) {
		dbt.mustExec("CREATE TABLE test (v INTEGER)")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		tx, err := dbt.db.BeginTx(ctx, &sql.TxOptions{
			ReadOnly: true,
		})
		if _, ok := err.(*MySQLError); ok {
			dbt.Skip("It seems that your MySQL does not support READ ONLY transactions")
			return
		} else if err != nil {
			dbt.Fatal(err)
		}

		// INSERT queries fail in a READ ONLY transaction.
		_, err = tx.ExecContext(ctx, "INSERT INTO test VALUES (1)")
		if _, ok := err.(*MySQLError); !ok {
			dbt.Errorf("expected MySQLError, got %v", err)
		}

		// SELECT queries can be executed.
		var v int
		row := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM test")
		if err := row.Scan(&v); err != nil {
			dbt.Fatal(err)
		}
		if v != 0 {
			dbt.Errorf("expected val to be 0, got %d", v)
		}

		if err := tx.Commit(); err != nil {
			dbt.Fatal(err)
		}
	})
}

func TestRowsColumnTypes(t *testing.T) {
	niNULL := sql.NullInt64{Int64: 0, Valid: false}
	ni0 := sql.NullInt64{Int64: 0, Valid: true}
	ni1 := sql.NullInt64{Int64: 1, Valid: true}
	ni42 := sql.NullInt64{Int64: 42, Valid: true}
	nfNULL := sql.NullFloat64{Float64: 0.0, Valid: false}
	nf0 := sql.NullFloat64{Float64: 0.0, Valid: true}
	nf1337 := sql.NullFloat64{Float64: 13.37, Valid: true}
	rbNULL := sql.RawBytes(nil)
	rb0 := sql.RawBytes("0")
	rb42 := sql.RawBytes("42")
	rbTest := sql.RawBytes("Test")

	var columns = []struct {
		name             string
		fieldType        string // type used when creating table schema
		databaseTypeName string // actual type used by MySQL
		scanType         reflect.Type
		nullable         bool
		length           int64 // 0 if not ok, BYTE length, not CHAR length
		precision        int64 // 0 if not ok
		valuesIn         [3]string
		valuesOut        [3]interface{}
	}{
		{"boolnull", "BOOL", "TINYINT", scanTypeNullInt, true, 1, 0, [3]string{"NULL", "true", "0"}, [3]interface{}{niNULL, ni1, ni0}},
		{"bool", "BOOL NOT NULL", "TINYINT", scanTypeInt8, false, 1, 0, [3]string{"1", "0", "FALSE"}, [3]interface{}{int8(1), int8(0), int8(0)}},
		{"intnull", "INTEGER", "INT", scanTypeNullInt, true, 11, 0, [3]string{"0", "NULL", "42"}, [3]interface{}{ni0, niNULL, ni42}},
		{"int3null", "INT(3)", "INT", scanTypeNullInt, true, 3, 0, [3]string{"0", "NULL", "42"}, [3]interface{}{ni0, niNULL, ni42}},
		{"int7", "INT(7) NOT NULL", "INT", scanTypeInt32, false, 7, 0, [3]string{"0", "-1337", "42"}, [3]interface{}{int32(0), int32(-1337), int32(42)}},
		{"uint13", "INT(13) UNSIGNED NOT NULL", "INT", scanTypeUint32, false, 13, 0, [3]string{"0", "1337", "42"}, [3]interface{}{uint32(0), uint32(1337), uint32(42)}},
		{"doublenull", "DOUBLE", "DOUBLE", scanTypeNullFloat, true, 22, 31, [3]string{"0", "NULL", "13.37"}, [3]interface{}{nf0, nfNULL, nf1337}},
		{"char25null", "CHAR(25)", "CHAR", scanTypeRawBytes, true, 75, 0, [3]string{"0", "NULL", "'Test'"}, [3]interface{}{rb0, rbNULL, rbTest}},
		{"varchar42", "VARCHAR(42) NOT NULL", "VARCHAR", scanTypeRawBytes, false, 126, 0, [3]string{"0", "'Test'", "42"}, [3]interface{}{rb0, rbTest, rb42}},
		{"textnull", "TEXT", "BLOB", scanTypeRawBytes, true, 196605, 0, [3]string{"0", "NULL", "'Test'"}, [3]interface{}{rb0, rbNULL, rbTest}},
		{"longtext", "LONGTEXT NOT NULL", "BLOB", scanTypeRawBytes, false, 4294967295, 0, [3]string{"0", "'Test'", "42"}, [3]interface{}{rb0, rbTest, rb42}},
	}

	schema := ""
	values1 := ""
	values2 := ""
	values3 := ""
	for _, column := range columns {
		schema += fmt.Sprintf("`%s` %s, ", column.name, column.fieldType)
		values1 += column.valuesIn[0] + ", "
		values2 += column.valuesIn[1] + ", "
		values3 += column.valuesIn[2] + ", "
	}
	schema = schema[:len(schema)-2]
	values1 = values1[:len(values1)-2]
	values2 = values2[:len(values2)-2]
	values3 = values3[:len(values3)-2]

	runTests(t, dsn, func(dbt *DBTest) {
		dbt.mustExec("CREATE TABLE test (" + schema + ")")
		dbt.mustExec("INSERT INTO test VALUES (" + values1 + "), (" + values2 + "), (" + values3 + ")")

		rows, err := dbt.db.Query("SELECT * FROM test")
		if err != nil {
			t.Fatalf("Query: %v", err)
		}

		tt, err := rows.ColumnTypes()
		if err != nil {
			t.Fatalf("ColumnTypes: %v", err)
		}

		if len(tt) != len(columns) {
			t.Fatalf("unexpected number of columns: expected %d, got %d", len(columns), len(tt))
		}

		types := make([]reflect.Type, len(tt))
		for i, tp := range tt {
			column := columns[i]

			// Name
			name := tp.Name()
			if name != column.name {
				t.Errorf("column name mismatch %s != %s", name, column.name)
				continue
			}

			// DatabaseTypeName
			databaseTypeName := tp.DatabaseTypeName()
			if databaseTypeName != column.databaseTypeName {
				t.Errorf("databasetypename name mismatch for column %q: %s != %s", name, databaseTypeName, column.databaseTypeName)
				continue
			}

			// ScanType
			scanType := tp.ScanType()
			if scanType != column.scanType {
				if scanType == nil {
					t.Errorf("scantype is null for column %q", name)
				} else {
					t.Errorf("scantype mismatch for column %q: %s != %s", name, scanType.Name(), column.scanType.Name())
				}
				continue
			}
			types[i] = scanType

			// Nullable
			nullable, ok := tp.Nullable()
			if !ok {
				t.Errorf("nullable not ok %q", name)
				continue
			}
			if nullable != column.nullable {
				t.Errorf("nullable mismatch for column %q: %t != %t", name, nullable, column.nullable)
			}

			// Length
			length, ok := tp.Length()
			if length != column.length {
				if !ok {
					t.Errorf("length not ok for column %q", name)
				} else {
					t.Errorf("length mismatch for column %q: %d != %d", name, length, column.length)
				}
				continue
			}

			// Precision
			precision, _, ok := tp.DecimalSize()
			if precision != column.precision {
				if !ok {
					t.Errorf("precision not ok for column %q", name)
				} else {
					t.Errorf("precision mismatch for column %q: %d != %d", name, precision, column.precision)
				}
				continue
			}
		}

		values := make([]interface{}, len(tt))
		for i := range values {
			values[i] = reflect.New(types[i]).Interface()
		}
		i := 0
		for rows.Next() {
			err = rows.Scan(values...)
			if err != nil {
				t.Fatalf("failed to scan values in %v", err)
			}
			for j := range values {
				value := reflect.ValueOf(values[j]).Elem().Interface()
				if !reflect.DeepEqual(value, columns[j].valuesOut[i]) {
					fmt.Println(value, columns[j].valuesOut[i])
					t.Errorf("row %d, column %d: %v != %v", i, j, value, columns[j].valuesOut[i])
				}
			}
			i++
		}
		if i != 3 {
			t.Errorf("expected 3 rows, got %d", i)
		}

		if err := rows.Close(); err != nil {
			t.Errorf("error closing rows: %s", err)
		}
	})
}
