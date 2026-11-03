package vector

import (
	"context"
	"testing"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/infra/circuitbreaker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type testVectorCircuit struct {
	err      error
	outcomes []circuitbreaker.Outcome
}

func (circuit *testVectorCircuit) Acquire(_ context.Context, scope string, policy circuitbreaker.Policy) (circuitbreaker.Permit, error) {
	return circuitbreaker.Permit{Scope: scope, Policy: policy}, circuit.err
}
func (circuit *testVectorCircuit) Finish(_ context.Context, _ circuitbreaker.Permit, outcome circuitbreaker.Outcome) error {
	circuit.outcomes = append(circuit.outcomes, outcome)
	return nil
}

func TestVectorCircuitSeparatesServiceFailureFromMessageSize(t *testing.T) {
	for _, test := range []struct {
		code    codes.Code
		failed  bool
		message string
	}{{codes.Unavailable, true, "unavailable"}, {codes.Internal, true, "internal"}, {codes.DeadlineExceeded, true, "timeout"}, {codes.ResourceExhausted, true, "overloaded"}, {codes.ResourceExhausted, false, "grpc: received message larger than max (5000000 vs. 4194304)"}, {codes.InvalidArgument, false, "invalid"}} {
		circuit := &testVectorCircuit{}
		invoke := func(context.Context, string, interface{}, interface{}, *grpc.ClientConn, ...grpc.CallOption) error {
			return status.Error(test.code, test.message)
		}
		err := circuitInterceptor(circuit, "ahnlich:test")(t.Context(), "GetSimN", nil, nil, nil, invoke)
		if err == nil || len(circuit.outcomes) != 1 || circuit.outcomes[0].Failed != test.failed {
			t.Fatalf("code=%s error=%v outcomes=%v", test.code, err, circuit.outcomes)
		}
	}
	circuit := &testVectorCircuit{err: &circuitbreaker.PauseError{Scope: "ahnlich:test", Reason: "vector_service_unavailable", After: time.Minute}}
	called := false
	invoke := func(context.Context, string, interface{}, interface{}, *grpc.ClientConn, ...grpc.CallOption) error {
		called = true
		return nil
	}
	err := circuitInterceptor(circuit, "ahnlich:test")(t.Context(), "GetSimN", nil, nil, nil, invoke)
	if _, paused := circuitbreaker.Paused(err); !paused || called {
		t.Fatalf("open circuit reached RPC: error=%v called=%t", err, called)
	}
}
