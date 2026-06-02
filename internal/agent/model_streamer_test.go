package agent

import (
	"errors"
	"testing"
)

func TestIsRecoverableProviderError_CodebaseInvalidMessageIsNotRecoverable(t *testing.T) {
	err := errors.New("codebase api error: trae_permanent_error(invalid params): We're sorry, the param is invalid.; biz error: rpc error: code = ErrParamInvalid desc = invalid message, origin err = invalid message (code=4001)")

	if isRecoverableProviderError(err) {
		t.Fatal("expected codebase invalid message error to be non-recoverable")
	}
}

func TestIsRecoverableProviderError_CodebaseOrdinaryParamErrorIsRecoverable(t *testing.T) {
	err := errors.New("codebase api error: missing required field repo_name (code=4001)")

	if !isRecoverableProviderError(err) {
		t.Fatal("expected ordinary codebase parameter error to remain recoverable")
	}
}
