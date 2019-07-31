package auth

import (
	"context"
	"io/ioutil"
	"os"
	"strings"

	"github.com/dgrijalva/jwt-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type authClient struct {
	publicKey []byte
}

func NewAuthClient(pkFile string) (*authClient, error) {
	f, err := os.Open(pkFile)
	if err != nil {
		return nil, err
	}

	bytes, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}
	return &authClient{publicKey: bytes}, nil
}

func (a *authClient) AuthInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {

	tknStr, ok := getToken(ctx)
	if !ok {
		return nil, status.Errorf(codes.Unauthenticated, "missing token")
	}
	// TODO(steeling): Modify to ParseWithClaims and inspect claims.
	_, err := jwt.Parse(tknStr, func(token *jwt.Token) (interface{}, error) {
		return a.publicKey, nil
	})

	if err != nil {
		return nil, status.Errorf(codes.Unauthenticated, "invalid token")
	}
	return handler(ctx, req)
}

func getToken(ctx context.Context) (string, bool) {
	headers, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", false
	}
	authHeader, ok := headers["authorization"]
	if !ok {
		return "", false
	}
	token := authHeader[0]
	// Remove Bearer
	tokenParts := strings.Split(token, "Bearer ")
	token = tokenParts[len(tokenParts)-1]
	return token, true
}
