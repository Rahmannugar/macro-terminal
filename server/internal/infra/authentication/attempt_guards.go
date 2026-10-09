package authentication

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Rahmannugar/authlier/emailpassword"
	"github.com/Rahmannugar/authlier/emailverification"
	"github.com/Rahmannugar/authlier/passwordreset"
	"github.com/Rahmannugar/macro-terminal/server/internal/infra/ratelimit"
)

var (
	signUpPerIPLimit = ratelimit.Rule{
		ImmediateRequests:            5,
		RestoreImmediateRequestsOver: time.Hour,
		MaximumRequests:              10,
		MaximumWindow:                time.Hour,
	}
	signUpPerEmailLimit = ratelimit.Rule{
		ImmediateRequests:            2,
		RestoreImmediateRequestsOver: time.Hour,
		MaximumRequests:              3,
		MaximumWindow:                time.Hour,
	}
	signInPerIPLimit = ratelimit.Rule{
		ImmediateRequests:            10,
		RestoreImmediateRequestsOver: 5 * time.Minute,
		MaximumRequests:              30,
		MaximumWindow:                15 * time.Minute,
	}
	signInPerEmailLimit = ratelimit.Rule{
		ImmediateRequests:            5,
		RestoreImmediateRequestsOver: 15 * time.Minute,
		MaximumRequests:              10,
		MaximumWindow:                15 * time.Minute,
	}
	verificationRequestPerIPLimit = ratelimit.Rule{
		ImmediateRequests:            10,
		RestoreImmediateRequestsOver: 15 * time.Minute,
		MaximumRequests:              20,
		MaximumWindow:                15 * time.Minute,
	}
	verificationRequestPerEmailLimit = ratelimit.Rule{
		ImmediateRequests:            3,
		RestoreImmediateRequestsOver: 15 * time.Minute,
		MaximumRequests:              5,
		MaximumWindow:                15 * time.Minute,
	}
	verificationCheckPerIPLimit = ratelimit.Rule{
		ImmediateRequests:            15,
		RestoreImmediateRequestsOver: 10 * time.Minute,
		MaximumRequests:              30,
		MaximumWindow:                10 * time.Minute,
	}
	verificationCheckPerEmailLimit = ratelimit.Rule{
		ImmediateRequests:            5,
		RestoreImmediateRequestsOver: 10 * time.Minute,
		MaximumRequests:              10,
		MaximumWindow:                10 * time.Minute,
	}
)

type PasswordAttemptGuard struct {
	limiter *ratelimit.RedisLimiter
}

func NewPasswordAttemptGuard(limiter *ratelimit.RedisLimiter) *PasswordAttemptGuard {
	return &PasswordAttemptGuard{limiter: limiter}
}

func (guard *PasswordAttemptGuard) Check(
	ctx context.Context,
	attempt emailpassword.Attempt,
) error {
	operation := string(attempt.Operation)
	if err := guard.check(
		ctx,
		operation,
		"ip",
		clientIP(attempt.SourceKey),
		passwordIPLimit(attempt.Operation),
	); err != nil {
		return err
	}

	if email := strings.TrimSpace(attempt.Email); email != "" {
		return guard.check(ctx, operation, "email", email, passwordEmailLimit(attempt.Operation))
	}
	return nil
}

func passwordIPLimit(operation emailpassword.Operation) ratelimit.Rule {
	switch operation {
	case emailpassword.OperationRegister:
		return signUpPerIPLimit
	default:
		return signInPerIPLimit
	}
}

func passwordEmailLimit(operation emailpassword.Operation) ratelimit.Rule {
	if operation == emailpassword.OperationRegister {
		return signUpPerEmailLimit
	}
	return signInPerEmailLimit
}

func (guard *PasswordAttemptGuard) check(
	ctx context.Context,
	operation string,
	dimension string,
	identity string,
	rule ratelimit.Rule,
) error {
	_, err := guard.limiter.Allow(ctx, "authentication.password."+operation, dimension, identity, rule)
	if errors.Is(err, ratelimit.ErrLimitExceeded) {
		return emailpassword.ErrAttemptBlocked
	}
	return err
}

type OTPAttemptGuard struct {
	limiter *ratelimit.RedisLimiter
}

func NewOTPAttemptGuard(limiter *ratelimit.RedisLimiter) *OTPAttemptGuard {
	return &OTPAttemptGuard{limiter: limiter}
}

func (guard *OTPAttemptGuard) Check(
	ctx context.Context,
	attempt emailverification.Attempt,
) error {
	operation := string(attempt.Operation)
	ipLimit := verificationRequestPerIPLimit
	emailLimit := verificationRequestPerEmailLimit
	if attempt.Operation == emailverification.OperationVerify {
		ipLimit = verificationCheckPerIPLimit
		emailLimit = verificationCheckPerEmailLimit
	}

	if err := guard.check(ctx, operation, "ip", clientIP(attempt.SourceKey), ipLimit); err != nil {
		return err
	}
	if email := strings.TrimSpace(attempt.Email); email != "" {
		return guard.check(ctx, operation, "email", email, emailLimit)
	}
	return nil
}

func (guard *OTPAttemptGuard) check(
	ctx context.Context,
	operation string,
	dimension string,
	identity string,
	rule ratelimit.Rule,
) error {
	_, err := guard.limiter.Allow(ctx, "authentication.email_verification."+operation, dimension, identity, rule)
	if errors.Is(err, ratelimit.ErrLimitExceeded) {
		return emailverification.ErrAttemptBlocked
	}
	return err
}

type PasswordResetAttemptGuard struct {
	limiter *ratelimit.RedisLimiter
}

func NewPasswordResetAttemptGuard(limiter *ratelimit.RedisLimiter) *PasswordResetAttemptGuard {
	return &PasswordResetAttemptGuard{limiter: limiter}
}

func (guard *PasswordResetAttemptGuard) Check(
	ctx context.Context,
	attempt passwordreset.Attempt,
) error {
	operation := string(attempt.Operation)
	ipLimit := verificationRequestPerIPLimit
	emailLimit := verificationRequestPerEmailLimit
	if attempt.Operation == passwordreset.OperationReset {
		ipLimit = verificationCheckPerIPLimit
		emailLimit = verificationCheckPerEmailLimit
	}

	if err := guard.check(ctx, operation, "ip", clientIP(attempt.SourceKey), ipLimit); err != nil {
		return err
	}
	if email := strings.TrimSpace(attempt.Email); email != "" {
		return guard.check(ctx, operation, "email", email, emailLimit)
	}
	return nil
}

func (guard *PasswordResetAttemptGuard) check(
	ctx context.Context,
	operation string,
	dimension string,
	identity string,
	rule ratelimit.Rule,
) error {
	_, err := guard.limiter.Allow(ctx, "authentication.password_reset."+operation, dimension, identity, rule)
	if errors.Is(err, ratelimit.ErrLimitExceeded) {
		return passwordreset.ErrAttemptBlocked
	}
	return err
}

func clientIP(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return "unknown"
	}
	return source
}

var _ emailpassword.AttemptGuard = (*PasswordAttemptGuard)(nil)
var _ emailverification.AttemptGuard = (*OTPAttemptGuard)(nil)
var _ passwordreset.AttemptGuard = (*PasswordResetAttemptGuard)(nil)
