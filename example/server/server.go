package main

import (
	"context"
	"io"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "gitlab.alibaba-inc.com/moweng.xx/grpc-proxy/example"
)

type server struct{}

var _ pb.ExampleServer = &server{}

func (s *server) UnaryReqEmpty(ctx context.Context, in *pb.EmptyRequest) (*pb.HelloResponse, error) {
	return &pb.HelloResponse{
		Res: "?",
		Cnt: 0,
	}, nil
}

func (s *server) UnaryResEmpty(ctx context.Context, in *pb.HelloRequest) (*pb.EmptyResponse, error) {
	return &pb.EmptyResponse{}, nil
}

func (s *server) UnaryAllEmpty(ctx context.Context, in *pb.EmptyRequest) (*pb.EmptyResponse, error) {
	return &pb.EmptyResponse{}, nil
}

func (s *server) UnaryNonEmpty(ctx context.Context, in *pb.HelloRequest) (*pb.HelloResponse, error) {
	return &pb.HelloResponse{
		Res: in.Req,
		Cnt: 1,
	}, nil
}

func (s *server) StreamReq(stream pb.Example_StreamReqServer) error {
	var cnt int32
	var msg []string
	var err error
	for {
		var req *pb.HelloRequest
		req, err = stream.Recv()
		if err != nil {
			break
		}
		cnt++
		msg = append(msg, req.Req)
	}

	if err != nil && err != io.EOF {
		return err
	}

	return stream.SendAndClose(&pb.HelloResponse{
		Res: strings.Join(msg, ","),
		Cnt: cnt,
	})
}

func (s *server) StreamRes(in *pb.HelloRequest, stream pb.Example_StreamResServer) error {
	for i := 0; i < 10; i++ {
		if i == 5 {
			return status.Errorf(codes.FailedPrecondition, "intended error")
		}
		time.Sleep(time.Second)
		err := stream.Send(&pb.HelloResponse{
			Res: strconv.Itoa(i),
			Cnt: int32(i + 1),
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *server) StreamAll(stream pb.Example_StreamAllServer) error {
	var cnt int32
	var msg []string
	var err error
	for {
		var req *pb.HelloRequest
		req, err = stream.Recv()
		if err != nil {
			break
		}
		cnt++
		msg = append(msg, req.Req)
	}

	if err != nil && err != io.EOF {
		return err
	}

	for idx, ele := range msg {
		err := stream.Send(&pb.HelloResponse{
			Res: ele,
			Cnt: int32(idx + 1),
		})
		if err != nil {
			return err
		}
	}
	return nil
}
