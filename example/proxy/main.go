package main

import (
	"context"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strings"

	"github.com/golang/protobuf/proto"
	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_logrus "github.com/grpc-ecosystem/go-grpc-middleware/logging/logrus"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"gitlab.alibaba-inc.com/moweng.xx/grpc-proxy/example"
	"gitlab.alibaba-inc.com/moweng.xx/grpc-proxy/proxy"
)

func main() {
	port := "8888"
	if len(os.Args) >= 2 {
		port = strings.TrimSpace(os.Args[1])
	}

	logger := logrus.StandardLogger().WithField("pid", os.Getpid())
	logger.Logger.Formatter = &logrus.JSONFormatter{TimestampFormat: "2006/01/02 15:04:05"}
	logger.Logger.SetLevel(logrus.DebugLevel)

	s := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			grpc_middleware.ChainUnaryServer(
				grpc_prometheus.UnaryServerInterceptor,
				grpc_logrus.UnaryServerInterceptor(logger),
			),
		),
		grpc.StreamInterceptor(
			grpc_middleware.ChainStreamServer(
				grpc_prometheus.StreamServerInterceptor,
				grpc_logrus.StreamServerInterceptor(logger),
			),
		),
		grpc.CustomCodec(proxy.NewCodec()),
		grpc.UnknownServiceHandler(proxy.TransparentHandler(director)),
	)

	lis, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		logger.Fatalf("listen error: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	go func() {
		http.ListenAndServe(":5252", mux)
	}()

	logger.Infof("Server listening on :%s", port)
	logger.Fatal(s.Serve(lis))
}

func director(ctx context.Context, fullMethodName string, data []byte) (*proxy.StreamDestination, error) {
	conn1, err := grpc.Dial("127.0.0.1:9999",
		grpc.WithInsecure(),
		grpc.WithDefaultCallOptions(
			grpc.ForceCodec(proxy.NewCodec()),
		),
	)
	if err != nil {
		return nil, err
	}

	conn2, err := grpc.Dial("127.0.0.1:9998",
		grpc.WithInsecure(),
		grpc.WithDefaultCallOptions(
			grpc.ForceCodec(proxy.NewCodec()),
		),
	)
	if err != nil {
		return nil, err
	}

	conn3, err := grpc.Dial("127.0.0.1:9997",
		grpc.WithInsecure(),
		grpc.WithDefaultCallOptions(
			grpc.ForceCodec(proxy.NewCodec()),
		),
	)
	if err != nil {
		return nil, err
	}

	// mock some request changes in proxy
	if fullMethodName == "/example.Example/UnaryNonEmpty" {
		req := &example.HelloRequest{}
		if err := proto.Unmarshal(data, req); err != nil {
			return nil, err
		}

		req.Req = "proxying: " + req.Req
		data, err = proto.Marshal(req)
		if err != nil {
			return nil, err
		}
	}

	var ctx1, ctx2, ctx3 context.Context
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		ctx1 = metadata.NewOutgoingContext(ctx, md)
		ctx2 = metadata.NewOutgoingContext(ctx, md)
		ctx3 = metadata.NewOutgoingContext(ctx, md)
	}

	dst := proxy.NewStreamDestination(
		proxy.ModePickPrimary,
		[]proxy.Destination{
			{
				Ctx:  ctx1,
				Conn: conn1,
			},
			{
				Ctx:          ctx2,
				Conn:         conn2,
				UserOriented: true,
			},
			{
				Ctx:  ctx3,
				Conn: conn3,
			},
		},
		data,
		conn1.Close, conn2.Close, conn3.Close,
	)
	return dst, nil
}
