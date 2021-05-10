package main

import (
	"net"
	"os"
	"strings"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_logrus "github.com/grpc-ecosystem/go-grpc-middleware/logging/logrus"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"

	pb "gitlab.alibaba-inc.com/moweng.xx/grpc-proxy/example"
)

func main() {
	port := "8888"
	if len(os.Args) >= 2 {
		port = strings.TrimSpace(os.Args[1])
	}

	logger := logrus.StandardLogger().WithField("pid", os.Getpid())
	logger.Logger.Formatter = &logrus.JSONFormatter{TimestampFormat: "2006/01/02 15:04:05"}
	logger.Logger.SetLevel(logrus.DebugLevel)

	svr := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			grpc_middleware.ChainUnaryServer(
				grpc_logrus.UnaryServerInterceptor(logger),
			),
		),
		grpc.StreamInterceptor(
			grpc_middleware.ChainStreamServer(
				grpc_logrus.StreamServerInterceptor(logger),
			),
		),
	)
	pb.RegisterExampleServer(svr, &server{})

	lis, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		logger.Fatalf("listen error: %v", err)
	}

	logger.Infof("Server listening on :%s", port)
	logger.Fatal(svr.Serve(lis))
}
