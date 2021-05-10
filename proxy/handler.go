// Copyright 2017 Michal Witkowski. All Rights Reserved.
// See LICENSE for licensing terms.

package proxy

import (
	"fmt"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var proxyStreamDesc = &grpc.StreamDesc{
	ServerStreams: true,
	ClientStreams: true,
}

// RegisterService sets up a proxy handler for a particular gRPC service and method.
// The behaviour is the same as if you were registering a handler method, e.g. from a codegenerated pb.go file.
//
// This can *only* be used if the `server` also uses grpcproxy.CodecForServer() ServerOption.
func RegisterService(server *grpc.Server, director StreamDirector, serviceName string, methodNames ...string) {
	streamer := &handler{director}
	fakeDesc := &grpc.ServiceDesc{
		ServiceName: serviceName,
		HandlerType: (*interface{})(nil),
	}
	for _, m := range methodNames {
		streamDesc := grpc.StreamDesc{
			StreamName:    m,
			Handler:       streamer.handler,
			ServerStreams: true,
			ClientStreams: true,
		}
		fakeDesc.Streams = append(fakeDesc.Streams, streamDesc)
	}
	server.RegisterService(fakeDesc, streamer)
}

// TransparentHandler returns a handler that attempts to proxy all requests that are not registered in the server.
// The indented use here is as a transparent proxy, where the server doesn't know about the services implemented by the
// backends. It should be used as a `grpc.UnknownServiceHandler`.
//
// This can *only* be used if the `server` also uses grpcproxy.CodecForServer() ServerOption.
func TransparentHandler(director StreamDirector) grpc.StreamHandler {
	streamer := &handler{director}
	return streamer.handler
}

type handler struct {
	director StreamDirector
}

// handler is where the real magic of proxying happens.
// It is invoked like any gRPC server stream and uses the gRPC server framing to get and receive bytes from the wire,
// forwarding it to a ClientStream established against the relevant ClientConn.
func (s *handler) handler(srv interface{}, stream grpc.ServerStream) error {
	// little bit of gRPC internals never hurt anyone
	fullMethod, ok := grpc.MethodFromServerStream(stream)
	if !ok {
		return grpc.Errorf(codes.Internal, "can not find grpc method in ServerStream")
	}

	// peek the first message from stream, we always use magic on the first gRPC request
	// we use the term `peek` because the message will be put on the wire again later
	pioneer := &frame{}
	if err := stream.RecvMsg(pioneer); err != nil {
		return err
	}
	// We require that the director's returned context inherits from the serverStream.Context().
	// `pioneer` is passed to director to give a chance for further modifications
	dst, err := s.director(stream.Context(), fullMethod, pioneer.payload)
	if err != nil {
		return err
	}
	defer func() {
		for _, f := range dst.finalizers {
			if err := f(); err != nil {
				fmt.Println(err)
			}
		}
	}()
	err = validateStreamDestination(dst)
	if err != nil {
		return err
	}

	hub, err := newStreamHub(fullMethod, dst)
	if err != nil {
		return err
	}
	defer hub.cancel()

	c2sErrChan := hub.proxyToServer(stream)
	s2cErrChan := hub.proxyToClient(stream)
	s2nErrChan := hub.proxyToNobody()

	var s2cFinished, s2bFinished bool
	// c2sErr: error when proxying client to server
	// s2cErr: error when proxying full duplex server to client
	// s2nErr: error when proxying half duplex server(s) to nothing(dropping)
	var c2sErr, s2cErr, s2nErr error
	for !(s2cFinished && s2bFinished) {
		select {
		case c2sErr = <-c2sErrChan:
			if c2sErr != nil {
				hub.cancel()
			}
		case s2nErr = <-s2nErrChan:
			s2bFinished = true
		case s2cErr = <-s2cErrChan:
			// This happens when the clientStream has nothing else to offer (io.EOF) or
			// returned a gRPC error. In those two cases we may have received Trailers
			// as part of the call. In case of other errors (stream closed) the trailers
			// will be nil.
			hub.setTrailer(stream)

			// clientErr will contain RPC error from client code. If not io.EOF, return
			// the RPC error as server stream error after the loop.
			if s2cErr != io.EOF {
				// If there is an error from the primary, we want to cancel all streams
				hub.cancel()
			}

			s2cFinished = true
		}
	}

	if c2sErr != nil {
		return status.Errorf(codes.Internal, "failed proxying client to server: %v", c2sErr)
	}
	if s2cErr != nil && s2cErr != io.EOF {
		return s2cErr
	}
	if s2nErr != nil {
		return status.Errorf(codes.Internal, "failed proxying between half duplex servers: %v", s2nErr)
	}

	return nil
}

func validateStreamDestination(dst *StreamDestination) error {
	if dst == nil || len(dst.replications) == 0 {
		return status.Errorf(codes.Unavailable, "no replications for proxying")
	}

	primaryCnt := 0
	for _, dst := range dst.replications {
		if dst.UserOriented == true {
			primaryCnt++
		}
	}
	if primaryCnt != 1 {
		return status.Errorf(codes.Unavailable, "exactly 1 full duplex replication needed")
	}

	return nil
}
