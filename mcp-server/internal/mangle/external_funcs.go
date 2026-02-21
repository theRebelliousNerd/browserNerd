package mangle

import (
	"fmt"
	"math"

	"codeberg.org/TauCeti/mangle-go/ast"
)

// DistanceCallback is an external predicate callback calculating 2D Euclidean distance.
// Predicate shape: my_distance(X1, Y1, X2, Y2, Dist_out)
type DistanceCallback struct{}

func (d DistanceCallback) ShouldPushdown() bool {
	return false
}

func (d DistanceCallback) ShouldQuery(inputs []ast.Constant, filters []ast.BaseTerm, pushdown []ast.Term) bool {
	_, _, _, _, ok := resolveDistanceOperands(inputs, filters)
	return ok
}

func (d DistanceCallback) ExecuteQuery(inputs []ast.Constant, filters []ast.BaseTerm, pushdown []ast.Term, cb func(output []ast.BaseTerm)) error {
	x1, y1, x2, y2, ok := resolveDistanceOperands(inputs, filters)
	if !ok {
		return nil
	}

	dist := math.Sqrt(math.Pow(x1-x2, 2) + math.Pow(y1-y2, 2))

	// Legacy mode(+,+,+,+,-): only output distance.
	if len(inputs) == 4 {
		cb([]ast.BaseTerm{ast.Float64(dist)})
		return nil
	}

	// Safe mode(-,-,-,-,-): emit all terms, preserving first four bound constants.
	x1Term, y1Term, x2Term, y2Term, err := extractDistanceConstants(filters)
	if err != nil {
		return nil
	}
	cb([]ast.BaseTerm{x1Term, y1Term, x2Term, y2Term, ast.Float64(dist)})
	return nil
}

func getFloatValue(c ast.Constant) (float64, error) {
	if c.Type == ast.Float64Type {
		f, _ := c.Float64Value()
		return f, nil
	}
	if c.Type == ast.NumberType {
		n, _ := c.NumberValue()
		return float64(n), nil
	}
	return 0, fmt.Errorf("expected number or float, got %v", c.Type)
}

func resolveDistanceOperands(inputs []ast.Constant, filters []ast.BaseTerm) (float64, float64, float64, float64, bool) {
	if len(inputs) == 4 {
		x1, err := getFloatValue(inputs[0])
		if err != nil {
			return 0, 0, 0, 0, false
		}
		y1, err := getFloatValue(inputs[1])
		if err != nil {
			return 0, 0, 0, 0, false
		}
		x2, err := getFloatValue(inputs[2])
		if err != nil {
			return 0, 0, 0, 0, false
		}
		y2, err := getFloatValue(inputs[3])
		if err != nil {
			return 0, 0, 0, 0, false
		}
		return x1, y1, x2, y2, true
	}

	x1Term, y1Term, x2Term, y2Term, err := extractDistanceConstants(filters)
	if err != nil {
		return 0, 0, 0, 0, false
	}
	x1, err := getFloatValue(x1Term)
	if err != nil {
		return 0, 0, 0, 0, false
	}
	y1, err := getFloatValue(y1Term)
	if err != nil {
		return 0, 0, 0, 0, false
	}
	x2, err := getFloatValue(x2Term)
	if err != nil {
		return 0, 0, 0, 0, false
	}
	y2, err := getFloatValue(y2Term)
	if err != nil {
		return 0, 0, 0, 0, false
	}
	return x1, y1, x2, y2, true
}

func extractDistanceConstants(filters []ast.BaseTerm) (ast.Constant, ast.Constant, ast.Constant, ast.Constant, error) {
	var zero ast.Constant
	if len(filters) < 4 {
		return zero, zero, zero, zero, fmt.Errorf("distance expected at least 4 filters, got %d", len(filters))
	}

	x1, ok := filters[0].(ast.Constant)
	if !ok {
		return zero, zero, zero, zero, fmt.Errorf("x1 is not bound")
	}
	y1, ok := filters[1].(ast.Constant)
	if !ok {
		return zero, zero, zero, zero, fmt.Errorf("y1 is not bound")
	}
	x2, ok := filters[2].(ast.Constant)
	if !ok {
		return zero, zero, zero, zero, fmt.Errorf("x2 is not bound")
	}
	y2, ok := filters[3].(ast.Constant)
	if !ok {
		return zero, zero, zero, zero, fmt.Errorf("y2 is not bound")
	}
	return x1, y1, x2, y2, nil
}
