// Copyright 2017 Michal Witkowski. All Rights Reserved.
// See LICENSE for licensing terms.

package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/goleak"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "gitlab.alibaba-inc.com/moweng.xx/grpc-proxy/testservice"
)

const (
	pingDefaultValue   = "I like kittens."
	clientMdKey        = "test-client-header"
	serverHeaderMdKey  = "test-client-header"
	serverTrailerMdKey = "test-client-trailer"

	rejectingMdKey = "test-reject-rpc-if-in-context"

	countListResponses = 20
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// asserting service is implemented on the server side and serves as a handler for stuff
type assertingService struct {
	t *testing.T
}

func (s *assertingService) PingEmpty(ctx context.Context, _ *pb.Empty) (*pb.PingResponse, error) {
	// Check that this call has client's metadata.
	md, ok := metadata.FromIncomingContext(ctx)
	assert.True(s.t, ok, "PingEmpty call must have metadata in context")
	_, ok = md[clientMdKey]
	assert.True(s.t, ok, "PingEmpty call must have clients's custom headers in metadata")
	return &pb.PingResponse{Value: pingDefaultValue, Counter: 42}, nil
}

func (s *assertingService) Ping(ctx context.Context, ping *pb.PingRequest) (*pb.PingResponse, error) {
	// Send user trailers and headers.
	grpc.SendHeader(ctx, metadata.Pairs(serverHeaderMdKey, "I like turtles."))
	grpc.SetTrailer(ctx, metadata.Pairs(serverTrailerMdKey, "I like ending turtles."))
	return &pb.PingResponse{Value: ping.Value, Counter: 42}, nil
}

func (s *assertingService) PingError(ctx context.Context, ping *pb.PingRequest) (*pb.Empty, error) {
	return nil, grpc.Errorf(codes.FailedPrecondition, "Userspace error.")
}

func (s *assertingService) PingList(ping *pb.PingRequest, stream pb.TestService_PingListServer) error {
	// Send user trailers and headers.
	require.NoError(s.t, stream.SendHeader(metadata.Pairs(serverHeaderMdKey, "I like turtles.")))
	for i := 0; i < countListResponses; i++ {
		stream.Send(&pb.PingResponse{Value: ping.Value, Counter: int32(i)})
	}
	stream.SetTrailer(metadata.Pairs(serverTrailerMdKey, "I like ending turtles."))
	return nil
}

func (s *assertingService) PingStream(stream pb.TestService_PingStreamServer) error {
	require.NoError(s.t, stream.SendHeader(metadata.Pairs(serverHeaderMdKey, "I like turtles.")))
	counter := int32(0)
	for {
		ping, err := stream.Recv()
		if err == io.EOF {
			break
		} else if err != nil {
			require.NoError(s.t, err, "can't fail reading stream")
			return err
		}
		pong := &pb.PingResponse{Value: ping.Value, Counter: counter}
		if err := stream.Send(pong); err != nil {
			require.NoError(s.t, err, "can't fail sending back a pong")
		}
		counter++
	}
	stream.SetTrailer(metadata.Pairs(serverTrailerMdKey, "I like ending turtles."))
	return nil
}

// ProxyHappySuite tests the "happy" path of handling: that everything works in absence of connection issues.
type ProxyHappySuite struct {
	suite.Suite
	// server related
	server *grpc.Server
	// proxy related
	proxy   *grpc.Server
	connP2S *grpc.ClientConn // intermediate grpc conn from proxy to server
	// client related
	client  pb.TestServiceClient
	connC2P *grpc.ClientConn // grpc conn from client to proxy
}

func (s *ProxyHappySuite) ctx() (context.Context, context.CancelFunc) {
	// Make all RPC calls last at most 1 sec, meaning all async issues or deadlock will not kill tests.
	return context.WithTimeout(context.Background(), 120*time.Second)
}

func (s *ProxyHappySuite) TestPingEmptyCarriesClientMetadata() {
	ctx, cancel := s.ctx()
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(clientMdKey, "true"))
	out, err := s.client.PingEmpty(ctx, &pb.Empty{})
	require.NoError(s.T(), err, "PingEmpty should succeed without errors")
	require.Equal(s.T(), &pb.PingResponse{Value: pingDefaultValue, Counter: 42}, out)
}

func (s *ProxyHappySuite) TestPingEmpty_StressTest() {
	for i := 0; i < 50; i++ {
		s.TestPingEmptyCarriesClientMetadata()
	}
}

func (s *ProxyHappySuite) TestPingCarriesServerHeadersAndTrailers() {
	ctx, cancel := s.ctx()
	defer cancel()

	headerMd := make(metadata.MD)
	trailerMd := make(metadata.MD)
	// This is an awkward calling convention... but meh.
	out, err := s.client.Ping(ctx, &pb.PingRequest{Value: "foo"}, grpc.Header(&headerMd), grpc.Trailer(&trailerMd))
	require.NoError(s.T(), err, "Ping should succeed without errors")
	require.Equal(s.T(), &pb.PingResponse{Value: "foo", Counter: 42}, out)
	assert.Contains(s.T(), headerMd, serverHeaderMdKey, "server response headers must contain server data")
	assert.Len(s.T(), trailerMd, 1, "server response trailers must contain server data")
}

func (s *ProxyHappySuite) TestPingErrorPropagatesAppError() {
	ctx, cancel := s.ctx()
	defer cancel()

	_, err := s.client.PingError(ctx, &pb.PingRequest{Value: "foo"})
	require.Error(s.T(), err, "PingError should never succeed")
	assert.Equal(s.T(), codes.FailedPrecondition, status.Code(err))
	assert.Equal(s.T(), "Userspace error.", status.Convert(err).Message())
}

func (s *ProxyHappySuite) TestDirectorErrorIsPropagated() {
	ctx, cancel := s.ctx()
	defer cancel()

	// See SetupSuite where the StreamDirector has a special case.
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(rejectingMdKey, "true"))
	_, err := s.client.Ping(ctx, &pb.PingRequest{Value: "foo"})
	require.Error(s.T(), err, "Director should reject this RPC")
	assert.Equal(s.T(), codes.PermissionDenied, status.Code(err))
	assert.Equal(s.T(), "testing rejection", status.Convert(err).Message())
}

func (s *ProxyHappySuite) TestPingStream_FullDuplexWorks() {
	ctx, cancel := s.ctx()
	defer cancel()

	stream, err := s.client.PingStream(ctx)
	require.NoError(s.T(), err, "PingStream request should be successful.")

	for i := 0; i < countListResponses; i++ {
		ping := &pb.PingRequest{Value: fmt.Sprintf("foo:%d", i)}
		require.NoError(s.T(), stream.Send(ping), "sending to PingStream must not fail")
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if i == 0 {
			// Check that the header arrives before all entries.
			headerMd, err := stream.Header()
			require.NoError(s.T(), err, "PingStream headers should not error.")
			assert.Contains(s.T(), headerMd, serverHeaderMdKey, "PingStream response headers user contain metadata")
		}
		assert.EqualValues(s.T(), i, resp.Counter, "ping roundtrip must succeed with the correct id")
	}
	require.NoError(s.T(), stream.CloseSend(), "no error on close send")
	_, err = stream.Recv()
	require.Equal(s.T(), io.EOF, err, "stream should close with io.EOF, meaining OK")
	// Check that the trailer headers are here.
	trailerMd := stream.Trailer()
	assert.Len(s.T(), trailerMd, 1, "PingList trailer headers user contain metadata")
}

func (s *ProxyHappySuite) TestPingStream_StressTest() {
	for i := 0; i < 50; i++ {
		s.TestPingStream_FullDuplexWorks()
	}
}

func (s *ProxyHappySuite) SetupSuite() {
	var err error
	// listener of proxy and server
	var listenerP, listenerS net.Listener

	listenerP, err = net.Listen("tcp", "127.0.0.1:0")
	require.NoError(s.T(), err, "must be able to allocate a port for proxy")
	addrProxy := listenerP.Addr().String()

	listenerS, err = net.Listen("tcp", "127.0.0.1:0")
	require.NoError(s.T(), err, "must be able to allocate a port for server")
	addrServer := listenerS.Addr().String()

	// 1. setup server
	s.server = grpc.NewServer()
	pb.RegisterTestServiceServer(s.server, &assertingService{t: s.T()})

	// 2. setup of proxy
	s.connP2S, err = grpc.Dial(addrServer, grpc.WithInsecure(), grpc.WithDefaultCallOptions(grpc.ForceCodec(NewCodec())))
	require.NoError(s.T(), err, "must not error when creating grpc.ClientConn to server")
	director := func(ctx context.Context, fullMethod string, pioneer []byte) (*StreamDestination, error) {
		outCtx := ctx
		md, ok := metadata.FromIncomingContext(ctx)
		if ok {
			if _, exists := md[rejectingMdKey]; exists {
				return nil, status.Errorf(codes.PermissionDenied, "testing rejection")
			}
			outCtx = metadata.NewOutgoingContext(ctx, md)
		}
		dest := &StreamDestination{
			pioneer: pioneer,
			replications: []Destination{
				{Ctx: outCtx, Conn: s.connP2S, UserOriented: true},
			},
		}
		return dest, nil
	}
	s.proxy = grpc.NewServer(
		grpc.CustomCodec(NewCodec()),
		grpc.UnknownServiceHandler(TransparentHandler(director)),
	)
	// Ping handler is handled as an explicit registration and not as a TransparentHandler.
	RegisterService(s.proxy, director, "mwitkow.testproto.TestService", "Ping")

	// start proxy and server
	go func() {
		s.server.Serve(listenerS)
	}()
	go func() {
		s.proxy.Serve(listenerP)
	}()

	// 3. setup client
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	s.connC2P, err = grpc.DialContext(ctx, addrProxy, grpc.WithInsecure())
	require.NoError(s.T(), err, "must not error when creating grpc.ClientConn to proxy")
	s.client = pb.NewTestServiceClient(s.connC2P)
}

func (s *ProxyHappySuite) TearDownSuite() {
	if s.connC2P != nil {
		s.connC2P.Close()
	}
	if s.connP2S != nil {
		s.connP2S.Close()
	}
	if s.proxy != nil {
		s.proxy.Stop()
	}
	if s.server != nil {
		s.server.Stop()
	}
}

func TestProxyHappySuite(t *testing.T) {
	suite.Run(t, &ProxyHappySuite{})
}
