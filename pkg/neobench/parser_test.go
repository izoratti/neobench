package neobench

import (
	"bytes"
	"fmt"
	"github.com/stretchr/testify/assert"
	"math"
	"math/rand"
	"testing"
	"time"
)

func TestParseTpcBLike(t *testing.T) {
	vars := map[string]interface{}{"scale": int64(1)}
	script, err := Parse("builtin:tpcb-like", TPCBLike, 1)

	assert.NoError(t, err)
	uow, err := script.Eval(ScriptContext{
		Vars: vars,
		Rand: rand.New(rand.NewSource(1337)),
	})
	assert.NoError(t, err)
	if err != nil {
		return
	}
	params := map[string]interface{}{"aid": int64(90704), "bid": int64(1), "delta": int64(-3348), "scale": int64(1), "tid": int64(1)}
	assert.Equal(t, []Statement{
		{
			Query:  "MATCH (account:Account {aid:$aid}) \nSET account.balance = account.balance + $delta",
			Params: params,
		},
		{
			Query:  "MATCH (account:Account {aid:$aid}) RETURN account.balance",
			Params: params,
		},
		{
			Query:  "MATCH (teller:Tellers {tid: $tid}) SET teller.balance = teller.balance + $delta",
			Params: params,
		},
		{
			Query:  "MATCH (branch:Branch {bid: $bid}) SET branch.balance = branch.balance + $delta",
			Params: params,
		},
		{
			Query:  "CREATE (:History { tid: $tid, bid: $bid, aid: $aid, delta: $delta, mtime: timestamp() })",
			Params: params,
		},
	}, uow.Statements)
}

func TestSleep(t *testing.T) {
	vars := map[string]interface{}{"scale": int64(1)}
	script, err := Parse("sleep", `\set sleeptime 13
\sleep $sleeptime us
RETURN 1;`, 1)

	assert.NoError(t, err)
	uow, err := script.Eval(ScriptContext{
		Vars: vars,
		Rand: rand.New(rand.NewSource(1337)),
	})
	assert.NoError(t, err)
	assert.Equal(t, []Statement{
		{
			Query:  "RETURN 1",
			Params: map[string]interface{}{"sleeptime": int64(13), "scale": int64(1)},
		},
	}, uow.Statements)
}

func TestSleepDuration(t *testing.T) {
	tests := map[string]struct {
		expectSleepDuration time.Duration
		expectError         error
	}{
		"\\sleep 10": {
			expectSleepDuration: 10 * time.Second,
		},
		"\\sleep 10 s": {
			expectSleepDuration: 10 * time.Second,
		},
		"\\sleep 10s": {
			expectSleepDuration: 10 * time.Second,
		},
		"\\sleep 10 ms": {
			expectSleepDuration: 10 * time.Millisecond,
		},
		"\\sleep 10 us": {
			expectSleepDuration: 10 * time.Microsecond,
		},
		"\\sleep 10 days": {
			expectError: fmt.Errorf("\\sleep command must use 'us', 'ms', or 's' unit argument - or none. got: days (at testSleep:'\\sleep 10 days':1:15)"),
		},
	}

	for given, tc := range tests {
		given, tc := given, tc
		t.Run(given, func(t *testing.T) {
			script, err := Parse(fmt.Sprintf("testSleep:'%s'", given), given, 1)

			if tc.expectError != nil {
				assert.Equal(t, tc.expectError, err)
				return
			}

			assert.NoError(t, err)
			cmd := script.Commands[0].(SleepCommand)
			actualDurationBase, err := cmd.Duration.Eval(nil)
			assert.Equal(t, tc.expectSleepDuration, time.Duration(actualDurationBase.(int64))*cmd.Unit)
		})
	}
}

func TestExpressions(t *testing.T) {
	tc := map[string]interface{}{
		// Scalars
		"0":           int64(0),
		"-0":          int64(-0),
		"1":           int64(1),
		"9999999000":  int64(9999999000),
		"-9999999000": int64(-9999999000),

		// Single-operator arithmetic
		"1 * 2":     int64(2),
		"1 * 2 * 4": int64(8),
		"-1 * 1337": int64(-1337),

		"2 / 2":      float64(1),
		"16 / 2 / 2": float64(4),

		"1 + 2":     int64(3),
		"1 + 2 + 4": int64(7),
		"-1 + 1337": int64(1336),

		"1 - 2":     int64(-1),
		"1 - 2 - 4": int64(-5),
		"-1 - 1337": int64(-1338),

		// Mixed operator precedence
		"1 * 2 + 1":     int64(3),
		"1 + 1 * 2":     int64(3),
		"2 * 2 / 4":     float64(1),
		"2 / 2 * 4":     float64(4),
		"2 - 1 * 2 + 1": int64(1),

		// Parantheticals
		"1 * (2 + 1)":     int64(3),
		"(1 * (2 + 1))":   int64(3),
		"(1 * (2 + (1)))": int64(3),

		// Functions
		"abs(-17)":                       int64(17),
		"abs(-17.6)":                     17.6,
		"double(5432)":                   float64(5432),
		"double(5432.0)":                 float64(5432),
		"greatest(5, 4, 3, 2)":           int64(5),
		"greatest(-5, -4, -3, -2)":       int64(-2),
		"greatest(5, 4, 3, 2.0, 8)":      float64(8),
		"least(5, 4, 3, 2)":              int64(2),
		"least(5, 4, 3, 2.0, 8)":         2.0,
		"least(-5, -4, -3, -2)":          int64(-5),
		"int(5.4 + 3.8)":                 int64(9),
		"int(5 + 4)":                     int64(9),
		"pi()":                           math.Pi,
		"random(1, 5)":                   int64(3),
		"random_gaussian(1, 10, 2.5)":    int64(3),
		"random_exponential(1, 10, 2.5)": int64(4),
		"sqrt(2.0)":                      1.414213562,
	}

	for expr, expected := range tc {
		expr, expected := expr, expected
		t.Run(expr, func(t *testing.T) {
			vars := map[string]interface{}{"scale": int64(1)}
			script, err := Parse(fmt.Sprintf("expr:'%s'", expr), fmt.Sprintf(`\set v %s
RETURN 1;`, expr), 1)

			assert.NoError(t, err)
			if err != nil {
				return
			}
			uow, err := script.Eval(ScriptContext{
				Vars: vars,
				Rand: rand.New(rand.NewSource(1337)),
			})
			assert.NoError(t, err)
			actual := uow.Statements[0].Params["v"]
			if expectedFloat, ok := expected.(float64); ok {
				assert.InDelta(t, expectedFloat, actual, 0.00001)
			} else {
				assert.Equal(t, expected, actual)
			}
		})
	}
}

func TestDebugFunction(t *testing.T) {
	vars := map[string]interface{}{"scale": int64(1)}
	script, err := Parse("test:debug(..)", "\\set blah debug(1337) * 10\nRETURN 1;", 1)

	assert.NoError(t, err)
	if err != nil {
		return
	}

	stderr := bytes.NewBuffer(nil)
	uow, err := script.Eval(ScriptContext{
		Stderr: stderr,
		Vars:   vars,
		Rand:   rand.New(rand.NewSource(1337)),
	})
	assert.NoError(t, err)
	assert.Equal(t, int64(13370), uow.Statements[0].Params["blah"])
	assert.Equal(t, "1337\n", stderr.String())
}
