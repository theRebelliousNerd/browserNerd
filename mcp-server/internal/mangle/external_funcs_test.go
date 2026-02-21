package mangle

import (
	"testing"

	"codeberg.org/TauCeti/mangle-go/ast"
)

func TestDistanceCallbackShouldQueryWithBoundFilters(t *testing.T) {
	cb := DistanceCallback{}
	ok := cb.ShouldQuery(
		nil,
		[]ast.BaseTerm{
			ast.Float64(0),
			ast.Float64(0),
			ast.Float64(3),
			ast.Float64(4),
			ast.Variable{Symbol: "Dist"},
		},
		nil,
	)
	if !ok {
		t.Fatalf("expected ShouldQuery to accept bound distance operands")
	}
}

func TestDistanceCallbackShouldQueryRejectsUnboundFilters(t *testing.T) {
	cb := DistanceCallback{}
	ok := cb.ShouldQuery(
		nil,
		[]ast.BaseTerm{
			ast.Variable{Symbol: "X1"},
			ast.Variable{Symbol: "Y1"},
			ast.Variable{Symbol: "X2"},
			ast.Variable{Symbol: "Y2"},
			ast.Variable{Symbol: "Dist"},
		},
		nil,
	)
	if ok {
		t.Fatalf("expected ShouldQuery to reject unbound distance operands")
	}
}

func TestDistanceCallbackExecuteQueryEmitsFiveTermsInSafeMode(t *testing.T) {
	cb := DistanceCallback{}
	var outputs [][]ast.BaseTerm

	err := cb.ExecuteQuery(
		nil,
		[]ast.BaseTerm{
			ast.Float64(0),
			ast.Float64(0),
			ast.Float64(3),
			ast.Float64(4),
			ast.Variable{Symbol: "Dist"},
		},
		nil,
		func(out []ast.BaseTerm) {
			outputs = append(outputs, out)
		},
	)
	if err != nil {
		t.Fatalf("unexpected ExecuteQuery error: %v", err)
	}
	if len(outputs) != 1 {
		t.Fatalf("expected exactly 1 output tuple, got %d", len(outputs))
	}
	if len(outputs[0]) != 5 {
		t.Fatalf("expected safe mode to emit 5 terms, got %d", len(outputs[0]))
	}

	dist, ok := outputs[0][4].(ast.Constant)
	if !ok {
		t.Fatalf("expected distance output to be a constant")
	}
	distVal, err := dist.Float64Value()
	if err != nil {
		t.Fatalf("expected float64 distance value: %v", err)
	}
	if distVal != 5 {
		t.Fatalf("expected distance to equal 5, got %v", distVal)
	}
}
