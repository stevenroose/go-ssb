// SPDX-FileCopyrightText: 2021 The Go-SSB Authors
//
// SPDX-License-Identifier: MIT

package legacyinvites

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ssbc/go-muxrpc/v2"
)

// supplies create, use and other managment calls (maybe list and delete?)
type masterPlug struct {
	service *Service
}

func (p masterPlug) Name() string {
	return "invite"
}

func (p masterPlug) Method() muxrpc.Method {
	return muxrpc.Method{"invite"}
}

func (p masterPlug) Handler() muxrpc.Handler {
	return createHandler{
		service: p.service,
	}
}

type createHandler struct {
	service *Service
}

type CreateArguments struct {
	// how many times this invite should be useable
	Uses uint `json:"uses"`

	// a note to organize invites (also posted when used)
	Note string `json:"note,omitempty"`
}

func (createHandler) Handled(m muxrpc.Method) bool { return m.String() == "invite.create" }

func (h createHandler) HandleConnect(ctx context.Context, e muxrpc.Endpoint) {}

func (h createHandler) HandleCall(ctx context.Context, req *muxrpc.Request) {
	// parse passed arguments
	var args []CreateArguments

	if err := json.Unmarshal(req.RawArgs, &args); err != nil {
		req.CloseWithError(fmt.Errorf("unable to receive invite create payload: %w", err))
		return
	}

	a := args[0]

	if a.Uses == 0 {
		req.CloseWithError(fmt.Errorf("cant create invite with zero uses"))
		return
	}

	inv, err := h.service.Create(a.Uses, a.Note)
	if err != nil {
		req.CloseWithError(fmt.Errorf("failed to create invite"))
		return
	}

	req.Return(ctx, inv.String())
	h.service.logger.Log("invite", "created", "uses", a.Uses)
}
