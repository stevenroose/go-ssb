// SPDX-FileCopyrightText: 2021 The Go-SSB Authors
//
// SPDX-License-Identifier: MIT

package multilogs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/dgraph-io/badger/v3"
	librarian "github.com/ssbc/margaret/indexes"
	libbader "github.com/ssbc/margaret/indexes/badger"
	"github.com/ssbc/go-ssb/internal/storedrefs"
	"github.com/ssbc/go-ssb/private"
	"go.mindeco.de/log"
	"go.mindeco.de/log/level"
	refs "github.com/ssbc/go-ssb-refs"
)

type Members map[string]bool

// MembershipStore isn't strictly a multilog but putting it in package private gave cyclic import
type MembershipStore struct {
	logger log.Logger

	idx         librarian.SeqSetterIndex
	self        refs.FeedRef
	unboxer     *private.Manager
	combinedidx *CombinedIndex
}

var _ io.Closer = (*MembershipStore)(nil)

var keyPrefix = []byte("group-members")

// NewMembershipIndex tracks group/add-member messages and triggers re-reading box2 messages by the invited people that couldn't be read before.
func NewMembershipIndex(logger log.Logger, db *badger.DB, self refs.FeedRef, unboxer *private.Manager, comb *CombinedIndex) (*MembershipStore, librarian.SinkIndex) {
	var store = MembershipStore{
		logger: logger,

		idx:         libbader.NewIndexWithKeyPrefix(db, Members{}, keyPrefix),
		self:        self,
		unboxer:     unboxer,
		combinedidx: comb,
	}

	snk := librarian.NewSinkIndex(store.updateFn, store.idx)
	return &store, snk
}

func (mc MembershipStore) Close() error {
	return mc.idx.Close()
}

func (mc MembershipStore) updateFn(ctx context.Context, seq int64, val interface{}, idx librarian.SetterIndex) error {
	msg, ok := val.(refs.Message)
	if !ok {
		return fmt.Errorf("not a message: %T", val)
	}

	if msg.Author().Equal(mc.self) {
		// our own message - all is done already
		level.Debug(mc.logger).Log("msg", "skipping own invite")
		return nil
	}

	cleartext, err := mc.unboxer.DecryptMessage(msg)
	if err != nil {
		return nil // invalid message
	}

	var addMemberMsg private.GroupAddMember
	err = json.Unmarshal(cleartext, &addMemberMsg)
	if err != nil {
		return nil // invalid message
	}

	var groupID refs.MessageRef
	var newMembers []refs.FeedRef
	for _, r := range addMemberMsg.Recps {
		rcp, err := refs.ParseMessageRef(r)
		if err == nil && rcp.Algo() == refs.RefAlgoCloakedGroup {
			groupID = rcp
			continue
		}

		m, err := refs.ParseFeedRef(r)
		if err != nil {
			return nil // invalid message
		}
		newMembers = append(newMembers, m)
	}
	level.Debug(mc.logger).Log("msg", "new members",
		"author", msg.Author().ShortSigil(),
		"group", groupID.ShortSigil(),
		"members", fmt.Sprintf("%v", newMembers),
	)

	idxAddr := storedrefs.Message(groupID)
	state, err := mc.idx.Get(ctx, idxAddr)
	if err != nil {
		return err
	}

	statev, err := state.Value()
	if err != nil {
		return err
	}

	var currentMembers Members
	switch tv := statev.(type) {
	case Members:
		currentMembers = tv
	case librarian.UnsetValue:
		currentMembers = make(Members, 0)
	default:
		return fmt.Errorf("not a Member: %T", statev)
	}

	for _, nm := range newMembers {
		_, indexed := currentMembers[nm.String()]
		if indexed {
			level.Debug(mc.logger).Log("msg", "already indexed",
				"group", groupID.ShortSigil(),
				"who", nm,
			)
			continue
		}

		whoToIndex := nm
		if nm.Equal(mc.self) {
			// if the invite is for us, we need to add the new group key
			cloakedGroupID, err := mc.unboxer.Join(addMemberMsg.GroupKey, addMemberMsg.Root)
			if err != nil {
				return err
			}
			level.Debug(mc.logger).Log("event", "joined group", "id", cloakedGroupID.String())

			// if we are invited, we need to index the sending author
			whoToIndex = msg.Author()
		}
		level.Debug(mc.logger).Log("msg", "reindexing",
			"group", groupID.ShortSigil(),
			"whoToIndex", whoToIndex,
		)
		err = mc.combinedidx.Box2Reindex(whoToIndex)
		if err != nil {
			return err
		}

		// mark as indexed
		currentMembers[whoToIndex.String()] = true
	}

	err = mc.idx.Set(ctx, idxAddr, currentMembers)
	if err != nil {
		return err
	}

	return nil
}
