// Copyright 2017 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package entry

import (
	"github.com/google/keytransparency/core/crypto/commitments"
	"github.com/google/keytransparency/core/crypto/signatures"
	"github.com/google/keytransparency/core/mutator"

	"github.com/google/trillian/crypto/keyspb"
	"github.com/google/trillian/crypto/sigpb"

	"github.com/benlaurie/objecthash/go/objecthash"

	tpb "github.com/google/keytransparency/core/proto/keytransparency_v1_proto"
)

// Mutation provides APIs for manipulating entries.
type Mutation struct {
	userID, appID string
	data, nonce   []byte

	prevEntry *tpb.Entry
	entry     *tpb.Entry
}

// NewMutation creates a mutation object from a previous value which can be modified.
// To create a new value:
// - Create a new mutation for a user starting with the previous value with NewMutation.
// - Change the value with SetCommitment and ReplaceAuthorizedKeys.
// - Finalize the changes and create the mutation with SerializeAndSign.
func NewMutation(oldValue, index []byte, userID, appID string) (*Mutation, error) {
	prevEntry, err := FromLeafValue(oldValue)
	if err != nil {
		return nil, err
	}

	pej, err := objecthash.CommonJSONify(prevEntry)
	if err != nil {
		return nil, err
	}
	hash, err := objecthash.ObjectHash(pej)
	if err != nil {
		return nil, err
	}

	return &Mutation{
		userID:    userID,
		appID:     appID,
		prevEntry: prevEntry,
		entry: &tpb.Entry{
			Index:          index,
			AuthorizedKeys: prevEntry.GetAuthorizedKeys(),
			Previous:       hash[:],
			Commitment:     prevEntry.GetCommitment(),
		},
	}, nil
}

// SetCommitment updates entry to be a commitment to data.
func (m *Mutation) SetCommitment(data []byte) error {
	// Commit to profile.
	commitmentNonce, err := commitments.GenCommitmentKey()
	if err != nil {
		return err
	}
	m.data = data
	m.nonce = commitmentNonce
	m.entry.Commitment = commitments.Commit(m.userID, m.appID, data, commitmentNonce)
	return nil
}

// ReplaceAuthorizedKeys sets authorized keys to pubkeys.
// pubkeys must contain at least one key.
func (m *Mutation) ReplaceAuthorizedKeys(pubkeys []*keyspb.PublicKey) error {
	if got, want := len(pubkeys), 1; got < want {
		return mutator.ErrMissingKey
	}
	m.entry.AuthorizedKeys = pubkeys
	return nil
}

// SerializeAndSign produces the mutation.
func (m *Mutation) SerializeAndSign(signers []signatures.Signer) (*tpb.UpdateEntryRequest, error) {
	signedkv, err := m.sign(signers)
	if err != nil {
		return nil, err
	}

	// Check authorization.
	skv := *signedkv
	skv.Signatures = nil
	if err := verifyKeys(m.prevEntry.GetAuthorizedKeys(),
		m.entry.GetAuthorizedKeys(),
		skv,
		signedkv.GetSignatures()); err != nil {
		return nil, err
	}

	return &tpb.UpdateEntryRequest{
		UserId: m.userID,
		AppId:  m.appID,
		EntryUpdate: &tpb.EntryUpdate{
			Mutation: signedkv,
			Committed: &tpb.Committed{
				Key:  m.nonce,
				Data: m.data,
			},
		},
	}, nil
}

// Sign produces the SignedKV
func (m *Mutation) sign(signers []signatures.Signer) (*tpb.Entry, error) {
	m.entry.Signatures = nil
	sigs := make(map[string]*sigpb.DigitallySigned)
	for _, signer := range signers {
		sig, err := signer.Sign(m.entry)
		if err != nil {
			return nil, err
		}
		sigs[signer.KeyID()] = sig
	}

	m.entry.Signatures = sigs
	return m.entry, nil
}
