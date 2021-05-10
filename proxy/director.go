// Copyright 2017 Michal Witkowski. All Rights Reserved.
// See LICENSE for licensing terms.

package proxy

import (
	"context"

	"google.golang.org/grpc"
)

type Mode int

const (
	// ModePickPrimary proxies gRPC request to the primary replication
	ModePickPrimary Mode = iota
	// ModePickRandom proxies gRPC request to a random selected replication
	ModePickRandom
	// ModeInParallel proxies gRPC request to all replications in parallel
	// in this case `primary` node will be used to interact with client
	ModeInParallel
)

// StreamDirector returns a gRPC ClientConn to be used to forward the call to.
//
// The presence of the `Context` allows for rich filtering, e.g. based on Metadata (headers).
// If no handling is meant to be done, a `codes.NotImplemented` gRPC error should be returned.
//
// The context returned from this function should be the context for the *outgoing* (to backend) call. In case you want
// to forward any Metadata between the inbound request and outbound requests, you should do it manually. However, you
// *must* propagate the cancel function (`context.WithCancel`) of the inbound context to the one returned.
//
// It is worth noting that the StreamDirector will be fired *after* all server-side stream interceptors
// are invoked. So decisions around authorization, monitoring etc. are better to be handled there.
//
// See the rather rich example.
type StreamDirector func(ctx context.Context, fullMethod string, pioneer []byte) (*StreamDestination, error)

func NewStreamDestination(m Mode, replications []Destination, pioneer []byte, finalizers ...func() error) *StreamDestination {
	return &StreamDestination{
		pioneer:      pioneer,
		mode:         m,
		replications: replications,
		finalizers:   finalizers,
	}
}

// StreamDestination returns a group of destinations to proxy
type StreamDestination struct {
	pioneer      []byte // the first gRPC request that may be rewritten by us
	mode         Mode
	replications []Destination
	finalizers   []func() error
}

// Destination contains a client connection as well as a rewritten protobuf message
// The Ctx of a Destination should be the context for the *outgoing* (to backend) call. In case you
// want to forward any Metadata between the inbound request and outbound requests, you should do it
// manually. However, you *must* propagate the cancel function (`context.WithCancel`) of the inbound
// context to Ctx of a Destination.
type Destination struct {
	Ctx          context.Context
	Conn         *grpc.ClientConn
	UserOriented bool
}
