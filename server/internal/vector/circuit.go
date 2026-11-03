package vector

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/infra/circuitbreaker"
	"google.golang.org/grpc"
	grpccodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func circuitInterceptor(circuit circuitbreaker.Controller, scope string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, request, response interface{}, connection *grpc.ClientConn, invoke grpc.UnaryInvoker, options ...grpc.CallOption) error {
		policy := circuitbreaker.DefaultPolicy()
		policy.ProbeLease = requestWindow + 5*time.Second
		permit, err := circuit.Acquire(ctx, scope+":"+method, policy)
		if err != nil {
			return err
		}
		outcome := circuitbreaker.Outcome{Neutral: true}
		defer func() {
			reportCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			if err := circuit.Finish(reportCtx, permit, outcome); err != nil {
				slog.ErrorContext(reportCtx, "Could not record vector circuit outcome", "event", "circuit.state_unavailable", "dependency_scope", scope, "error", err)
			}
		}()
		callCtx, cancel := context.WithTimeout(ctx, requestWindow)
		defer cancel()
		err = invoke(callCtx, method, request, response, connection, options...)
		if err == nil {
			outcome = circuitbreaker.Outcome{}
		} else if !errors.Is(ctx.Err(), context.Canceled) {
			switch status.Code(err) {
			case grpccodes.Unavailable, grpccodes.Internal, grpccodes.DeadlineExceeded:
				outcome = circuitbreaker.Outcome{Failed: true, Reason: "vector_service_unavailable"}
			case grpccodes.ResourceExhausted:
				if !strings.Contains(status.Convert(err).Message(), "message larger than max") {
					outcome = circuitbreaker.Outcome{Failed: true, Reason: "vector_service_unavailable"}
				}
			}
		}
		return err
	}
}
