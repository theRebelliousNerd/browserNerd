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
	return true
}

func (d DistanceCallback) ExecuteQuery(inputs []ast.Constant, filters []ast.BaseTerm, pushdown []ast.Term, cb func(output []ast.BaseTerm)) error {
	if len(inputs) != 4 {
		return fmt.Errorf("distance expected 4 input arguments, got %d", len(inputs))
	}

	x1, err := getFloatValue(inputs[0])
	if err != nil {
		return err
	}
	y1, err := getFloatValue(inputs[1])
	if err != nil {
		return err
	}
	x2, err := getFloatValue(inputs[2])
	if err != nil {
		return err
	}
	y2, err := getFloatValue(inputs[3])
	if err != nil {
		return err
	}

	dist := math.Sqrt(math.Pow(x1-x2, 2) + math.Pow(y1-y2, 2))
	cb([]ast.BaseTerm{ast.Float64(dist)})
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
