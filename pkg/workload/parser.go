package workload

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"text/scanner"
)

type Workload struct {
	Readonly bool
	Scale    int64
	Commands []Command
	Rand     *rand.Rand
}

func (s *Workload) NewClient() ClientWorkload {
	return ClientWorkload{
		Readonly: s.Readonly,
		Scale:    s.Scale,
		Commands: s.Commands,
		Rand:     rand.New(rand.NewSource(s.Rand.Int63())),
	}
}

type ClientWorkload struct {
	Readonly bool
	Scale    int64
	Commands []Command
	Rand     *rand.Rand
}

func (s *ClientWorkload) Next() (UnitOfWork, error) {
	ctx := CommandContext{
		Vars: map[string]interface{}{
			"scale": s.Scale,
		},
		Rand: s.Rand,
	}

	uow := UnitOfWork{
		Readonly:   s.Readonly,
		Statements: nil,
	}

	for _, cmd := range s.Commands {
		if err := cmd.Execute(&ctx, &uow); err != nil {
			return uow, err
		}
	}

	return uow, nil
}

type UnitOfWork struct {
	Readonly   bool
	Statements []Statement
}

type Statement struct {
	Query  string
	Params map[string]interface{}
}

type CommandContext struct {
	Vars map[string]interface{}
	Rand *rand.Rand
}

type Command interface {
	Execute(ctx *CommandContext, uow *UnitOfWork) error
}

type QueryCommand struct {
	Query string
}

func (c QueryCommand) Execute(ctx *CommandContext, uow *UnitOfWork) error {
	params := make(map[string]interface{})
	for k, v := range ctx.Vars {
		params[k] = v
	}
	uow.Statements = append(uow.Statements, Statement{
		Query:  c.Query,
		Params: params,
	})
	return nil
}

type SetCommand struct {
	VarName    string
	Expression Expression
}

func (c SetCommand) Execute(ctx *CommandContext, uow *UnitOfWork) error {
	value, err := c.Expression.Eval(ctx)
	if err != nil {
		return err
	}
	ctx.Vars[c.VarName] = value
	return nil
}

func Parse(filename, script string, scale, seed int64) (Workload, error) {
	var s scanner.Scanner
	s.Init(strings.NewReader(script))
	s.Filename = filename
	s.Whitespace ^= 1 << '\n' // don't skip newlines

	c := &context{
		s: s,
	}

	commands := make([]Command, 0)

	for !c.done {
		tok := c.Peek()
		if tok == scanner.EOF {
			break
		} else if tok == '\\' {
			commands = append(commands, metaCommand(c))
		} else if tok == '\n' {
			c.Next()
		} else {
			commands = append(commands, command(c))
		}
	}

	if c.err != nil {
		return Workload{}, c.err
	}

	return Workload{
		Readonly: false, // TODO
		Scale:    scale,
		Commands: commands,
		Rand:     rand.New(rand.NewSource(seed)),
	}, nil
}

func metaCommand(c *context) Command {
	expect(c, '\\')
	cmd := ident(c)

	switch cmd {
	case "set":
		varName := ident(c)
		setExpr := expr(c)
		return SetCommand{
			VarName:    varName,
			Expression: setExpr,
		}
	default:
		c.fail(fmt.Errorf("unexpected meta command: '%s'", cmd))
		return nil
	}
}

func command(c *context) Command {
	originalWhitespace := c.s.Whitespace
	defer func() {
		c.s.Whitespace = originalWhitespace
	}()
	c.s.Whitespace = 0
	var b strings.Builder
	for tok, content := c.Next(); tok != ';'; tok, content = c.Next() {
		b.WriteString(content)
	}
	return QueryCommand{
		Query: b.String(),
	}
}

func ident(c *context) string {
	tok, content := c.Next()
	if tok != scanner.Ident {
		c.fail(fmt.Errorf("expected identifier, got '%s'", scanner.TokenString(tok)))
	}
	return content
}

func expr(c *context) Expression {
	lhs := term(c)
	tok := c.Peek()
	if tok == '*' {
		c.Next()
		rhs := term(c)
		return Expression{
			Kind: callExpr,
			Payload: CallExpr{
				name: "*",
				args: []Expression{lhs, rhs},
			},
		}
	}
	return lhs
}

func term(c *context) Expression {
	lhs := factor(c)
	return lhs
}

func factor(c *context) Expression {
	tok, content := c.Next()
	if tok == scanner.Ident {
		funcName := content
		var args []Expression
		expect(c, '(')
		tok := c.Peek()
		for tok != ')' {
			if len(args) > 0 {
				expect(c, ',')
			}
			args = append(args, expr(c))
			if c.done {
				return Expression{}
			}
			tok = c.Peek()
		}
		c.Next()
		return Expression{Kind: callExpr, Payload: CallExpr{
			name: funcName,
			args: args,
		}}
	} else if tok == scanner.Int {
		intVal, err := strconv.Atoi(content)
		if err != nil {
			c.fail(err)
			return Expression{}
		}
		return Expression{Kind: intExpr, Payload: int64(intVal)}
	} else if tok == '-' {
		tok, content := c.Next()
		if tok == scanner.Int {
			intVal, err := strconv.Atoi(content)
			if err != nil {
				c.fail(err)
				return Expression{}
			}
			return Expression{Kind: intExpr, Payload: int64(-1 * intVal)}
		} else {
			c.fail(fmt.Errorf("unexpected token, expected integer after minus sign: %s", scanner.TokenString(tok)))
			return Expression{}
		}
	} else if tok == ':' {
		varName := ident(c)
		return Expression{Kind: varExpr, Payload: varName}
	} else {
		c.fail(fmt.Errorf("unexpected token, expected Expression: %s", scanner.TokenString(tok)))
		return Expression{}
	}
}

func expect(c *context, expected rune) {
	tok, _ := c.Next()
	if tok != expected {
		c.fail(fmt.Errorf("expected '%s', got '%s'", scanner.TokenString(expected), scanner.TokenString(tok)))
	}
}

type ExprKind uint8

const (
	nullExpr ExprKind = 0
	intExpr  ExprKind = 1
	callExpr ExprKind = 2
	varExpr  ExprKind = 3
)

func (e ExprKind) String() string {
	return exprKindNames[e]
}

var exprKindNames = []string{
	nullExpr: "N/A",
	intExpr:  "int",
	callExpr: "call",
	varExpr:  "var",
}

type Expression struct {
	Kind    ExprKind
	Payload interface{}
}

func (e Expression) Eval(ctx *CommandContext) (interface{}, error) {
	switch e.Kind {
	case intExpr:
		return e.Payload, nil
	case varExpr:
		value, found := ctx.Vars[e.Payload.(string)]
		if !found {
			return nil, fmt.Errorf("this variable is not defined: %s", e.Payload.(string))
		}
		return value, nil
	case callExpr:
		return e.Payload.(CallExpr).Eval(ctx)
	default:
		return nil, fmt.Errorf("unknown expression: %s", e.String())
	}
}

func (e Expression) String() string {
	switch e.Kind {
	case intExpr:
		return fmt.Sprintf("%d", e.Payload)
	case callExpr:
		return e.Payload.(CallExpr).String()
	case varExpr:
		return fmt.Sprintf(":%v", e.Payload)
	default:
		return fmt.Sprintf("err(%v)", e.Payload)
	}
}

type CallExpr struct {
	name string
	args []Expression
}

func (f CallExpr) String() string {
	args := make([]string, 0, len(f.args))
	for _, a := range f.args {
		args = append(args, a.String())
	}
	return fmt.Sprintf("%s(%s)", f.name, strings.Join(args, ", "))
}

func (f CallExpr) argAsNumber(i int, ctx *CommandContext) (Number, error) {
	if len(f.args) <= i {
		return Number{}, fmt.Errorf("expected at least %d arguments, got %d", i+1, len(f.args))
	}
	value, err := f.args[i].Eval(ctx)
	if err != nil {
		return Number{}, err
	}
	switch value.(type) {
	case int64:
		iVal := value.(int64)
		return Number{isFloat: false, val: float64(iVal), iVal: iVal}, nil
	case float64:
		return Number{isFloat: true, val: value.(float64)}, nil
	default:
		return Number{}, fmt.Errorf("expected int64 or float64, got %s (which is %T)", f.args[i].String(), value)
	}
}

func (f CallExpr) Eval(ctx *CommandContext) (interface{}, error) {
	switch f.name {
	case "random":
		a, err := f.argAsNumber(0, ctx)
		if err != nil {
			return nil, fmt.Errorf("in %s: %s", f.String(), err)
		}
		b, err := f.argAsNumber(1, ctx)
		if err != nil {
			return nil, fmt.Errorf("in %s: %s", f.String(), err)
		}

		if a.val == b.val {
			if a.isFloat {
				return a.val, nil
			} else {
				return a.iVal, nil
			}
		}

		if a.isFloat || b.isFloat {
			min, max := a.val, b.val
			return min + rand.Float64()*(max-min), nil
		} else {
			min, max := a.iVal, b.iVal
			return min + ctx.Rand.Int63n(max-min), nil
		}
	case "*":
		a, err := f.argAsNumber(0, ctx)
		if err != nil {
			return nil, fmt.Errorf("in %s: %s", f.String(), err)
		}
		b, err := f.argAsNumber(1, ctx)
		if err != nil {
			return nil, fmt.Errorf("in %s: %s", f.String(), err)
		}

		if a.isFloat || b.isFloat {
			return a.val * b.val, nil
		} else {
			return a.iVal * b.iVal, nil
		}
	default:
		return nil, fmt.Errorf("unknown function: %s", f.String())
	}
}

// Hacky first stab at dealing with runtime coercion, refactor as needed
type Number struct {
	isFloat bool
	// Always set
	val float64
	// Only set if isFloat == false
	iVal int64
}

type context struct {
	s scanner.Scanner
	// Next token returned by scanner, or 0
	peek     rune
	peekText string
	done     bool
	err      error
}

func (t *context) Peek() rune {
	if t.peek == 0 {
		t.peek = t.s.Scan()
		t.peekText = t.s.TokenText()
	}
	return t.peek
}

func (t *context) Next() (rune, string) {
	if t.peek != 0 {
		next := t.peek
		nextStr := t.peekText
		t.peek = 0
		if next == scanner.EOF {
			t.done = true
		}
		return next, nextStr
	}
	next := t.s.Scan()
	if next == scanner.EOF {
		t.done = true
	}
	return next, t.s.TokenText()
}

func (t *context) fail(err error) {
	t.done = true
	if t.err != nil {
		return
	}
	t.err = fmt.Errorf("%s (at %s)", err, t.s.Pos().String())
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}