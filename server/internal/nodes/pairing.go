package nodes

import (
	"context"
	"errors"
)

// pairForAddress exchanges a pairing code for a long-term token at
// the agent. An old-agent 404 becomes the upgrade message; a
// generic pairing failure becomes a validation error carrying the
// agent's message; anything else (unreachable, bad address) is an
// upstream error.
func (s *Service) pairForAddress(ctx context.Context, address, code string) (string, error) {
	token, err := s.client.Pair(ctx, address, code)
	if err != nil {
		if errors.Is(err, ErrPairingUnsupported) {
			return "", errValidation("节点 agent 版本过旧，不支持配对，请升级 agent 后再试")
		}
		if errors.Is(err, ErrPairingInvalid) {
			return "", errValidation(err.Error())
		}
		return "", errUpstream(truncate(err.Error(), 200))
	}
	return token, nil
}

// RequestPairingCode validates an address and asks the agent to
// generate a pairing code. It returns only ok/retry_after: the code
// itself is never in this response (it goes to the agent log).
// RequestPairingCode validates an address and asks the agent to
// generate a pairing code. It returns only ok/retry_after: the code
// itself is never in this response (it goes to the agent log).
func (s *Service) RequestPairingCode(ctx context.Context, address string) (bool, int, error) {
	addr, err := validateAddress(address)
	if err != nil {
		return false, 0, err
	}
	ok, retry, err := s.client.RequestPairingCode(ctx, addr)
	if err != nil {
		if errors.Is(err, ErrPairingUnsupported) {
			return false, 0, errValidation("节点 agent 版本过旧，不支持配对，请升级 agent 后再试")
		}
		return false, 0, errUpstream(truncate(err.Error(), 200))
	}
	return ok, retry, nil
}
