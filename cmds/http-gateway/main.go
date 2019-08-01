package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"time"

	"github.com/steeling/InterUSS-Platform/pkg/dssproto"
	"github.com/steeling/InterUSS-Platform/pkg/logging"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"go.uber.org/zap"

	"google.golang.org/grpc"
)

var (
	port        = flag.Int("port", 8080, "port")
	grpcBackend = flag.String("grpc-backend", "", "Endpoint for grpc backend. Only to be set if run in proxy mode")
)

// RunHTTPProxy starts the HTTP proxy for the DSS gRPC service on ctx, listening
// on port, proxying to endpoint.
func RunHTTPProxy(ctx context.Context, port int, endpoint string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Register gRPC server endpoint
	// Note: Make sure the gRPC server is running properly and accessible
	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithBlock(),
		grpc.WithTimeout(10 * time.Second),
	}

	err := dssproto.RegisterDiscoveryAndSynchronizationServiceHandlerFromEndpoint(ctx, mux, endpoint, opts)
	if err != nil {
		return err
	}

	// Start HTTP server (and proxy calls to gRPC server endpoint)
	return http.ListenAndServe(fmt.Sprintf(":%d", port), mux)
}

func main() {
	flag.Parse()
	var (
		ctx    = context.Background()
		logger = logging.WithValuesFromContext(ctx, logging.Logger)
	)

	if err := RunHTTPProxy(ctx, *port, *grpcBackend); err != nil {
		logger.Panic("Failed to execute service", zap.Error(err))
	}
	logger.Info("Shutting down gracefully")
}
