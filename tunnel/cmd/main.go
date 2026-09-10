package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	aipauth "github.com/unibaseio/aip-go-sdk/auth"
	aipserver "github.com/unibaseio/aip-go-sdk/server"
	x402 "github.com/x402-foundation/x402/go"
	x402http "github.com/x402-foundation/x402/go/http"
	ginmw "github.com/x402-foundation/x402/go/http/gin"
	evm "github.com/x402-foundation/x402/go/mechanisms/evm"
	evmexact "github.com/x402-foundation/x402/go/mechanisms/evm/exact/server"
	"go.uber.org/zap"

	"github.com/fabricfoundation/tunnel/config"
	"github.com/fabricfoundation/tunnel/internal"
	"github.com/fabricfoundation/tunnel/internal/aipagent"
	"github.com/fabricfoundation/tunnel/internal/attest"
	"github.com/fabricfoundation/tunnel/internal/handlers"
	"github.com/fabricfoundation/tunnel/internal/mppay"
)

func main() {
	configPath := flag.String("config", "config.json", "Path to config file")
	flag.Parse()

	logger, _ := zap.NewProduction()
	defer func() {
		if err := logger.Sync(); err != nil {
			logger.Warn("failed to sync logger", zap.Error(err))
		}
	}()

	if err := godotenv.Load(); err != nil {
		logger.Warn("failed to load .env file", zap.Error(err))
	}

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		logger.Fatal("configuration error", zap.Error(err))
	}

	// No token in the env? Fall back to the SDK's cached credentials, or walk
	// the user through the browser authorization flow on first run.
	if cfg.AIPEnabled && cfg.AIPPrivyToken == "" {
		token, wallet, err := aipauth.EnsureAuth(context.Background())
		if err != nil {
			logger.Fatal("unibase authorization failed", zap.Error(err))
		}
		cfg.AIPPrivyToken = token
		if cfg.AIPUserID == "" {
			cfg.AIPUserID = wallet
		}
		logger.Info("unibase authorization ready", zap.String("wallet", wallet))
	}

	if _, err := mppay.New(cfg, zap.NewNop()); err != nil {
		logger.Fatal("MPP configuration error", zap.Error(err))
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	stakingKey, err := cfg.StakingSigner()
	if err != nil {
		logger.Fatal("staking key is unusable", zap.Error(err))
	}

	aipSrv := aipagent.Build(cfg, handlers.PublishRobotAction, logger)
	if aipSrv != nil {
		go func() {
			if err := aipSrv.Run(ctx); err != nil {
				logger.Warn("AIP agent server stopped", zap.Error(err))
			}
		}()
	}

	for {
		router := setupRouter(cfg, aipSrv, logger)
		client := internal.NewClient(cfg.ProxyWSURL, cfg.RobotID, cfg.StakingAddress, stakingKey, router, logger)

		clientCtx, clientCancel := context.WithCancel(ctx)

		client.Run(clientCtx)
		clientCancel()

		if ctx.Err() != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// pristineNetworkConfigs is x402's built-in asset table, captured before anything overrides it,
// so the shipped default can be put back if nothing overrides it.
var pristineNetworkConfigs = func() map[string]evm.NetworkConfig {
	snapshot := make(map[string]evm.NetworkConfig, len(evm.NetworkConfigs))
	for network, cfg := range evm.NetworkConfigs {
		snapshot[network] = cfg
	}
	return snapshot
}()

// registeredNetwork is the network registerTokenAsset last overrode, so switching networks does
// not strand the previous one on a stale asset.
var registeredNetwork string

// restoreNetworkDefault undoes an override, falling back to whatever x402 shipped for the network.
func restoreNetworkDefault(network string) {
	if original, ok := pristineNetworkConfigs[network]; ok {
		evm.NetworkConfigs[network] = original
		return
	}
	delete(evm.NetworkConfigs, network)
}

// registerTokenAsset makes cfg.TokenAddress the default asset for cfg.Network. It runs on every
// (re)start of the router, so it has to undo whatever the previous config registered.
func registerTokenAsset(cfg *config.Config, logger *zap.Logger) {
	if registeredNetwork != "" && registeredNetwork != cfg.Network {
		restoreNetworkDefault(registeredNetwork)
		registeredNetwork = ""
	}

	if cfg.TokenAddress == "" {
		restoreNetworkDefault(cfg.Network)
		registeredNetwork = ""
		return
	}

	chainID, ok := cfg.ChainID()
	if !ok {
		logger.Warn("skipping token registration for non-eip155 network", zap.String("network", cfg.Network))
		return
	}

	asset := evm.AssetInfo{
		Address:  cfg.TokenAddress,
		Name:     cfg.TokenName,
		Version:  cfg.TokenVersion,
		Decimals: cfg.TokenDecimals,
	}

	if cfg.TokenTransferMethod == config.TransferMethodPermit2 {
		asset.AssetTransferMethod = evm.AssetTransferMethodPermit2
		asset.SupportsEip2612 = cfg.TokenSupportsEIP2612
	}

	evm.NetworkConfigs[cfg.Network] = evm.NetworkConfig{
		ChainID:      chainID,
		DefaultAsset: asset,
	}
	registeredNetwork = cfg.Network

	logger.Info("registered payment token",
		zap.String("network", cfg.Network),
		zap.String("address", cfg.TokenAddress),
		zap.String("name", cfg.TokenName),
		zap.Int("decimals", cfg.TokenDecimals),
		zap.String("transfer_method", cfg.TokenTransferMethod),
		zap.Bool("supports_eip2612", cfg.TokenSupportsEIP2612),
	)
}

func setupRouter(cfg *config.Config, aipSrv *aipserver.Server, logger *zap.Logger) *gin.Engine {
	registerTokenAsset(cfg, logger)

	router := gin.New()

	router.Use(cors.New(cors.Config{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders: []string{
			"Origin",
			"Content-Type",
			"Authorization",
			"PAYMENT-SIGNATURE",
			"Access-Control-Expose-Headers",
			"payment-signature",
		},
		ExposeHeaders: []string{
			"PAYMENT-REQUIRED",
			"PAYMENT-RESPONSE",
			"WWW-Authenticate",
			"Payment-Receipt",
			attest.HeaderName,
		},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	facilitatorClient := x402http.NewHTTPFacilitatorClient(&x402http.FacilitatorConfig{
		URL: cfg.FacilitatorURL,
	})

	routes := x402http.RoutesConfig{
		"POST /action": {
			Accepts: x402http.PaymentOptions{
				{
					Scheme:  "exact",
					Price:   cfg.Price,
					Network: x402.Network(cfg.Network),
					PayTo:   cfg.EVMPayeeAddress,
				},
			},
			Description: "Run a paid robot action",
			MimeType:    "application/json",
		},
	}

	x402Middleware := ginmw.X402Payment(ginmw.Config{
		Routes:      routes,
		Facilitator: facilitatorClient,
		Schemes: []ginmw.SchemeConfig{
			{Network: x402.Network(cfg.Network), Server: evmexact.NewExactEvmScheme()},
		},
		Timeout: 30 * time.Second,
	})

	// Registered before the payment middleware so the wrapped writer is in place
	// when the 402 is written. Signs whichever challenge the payer is served.
	if signer, signerErr := newRequirementsSigner(cfg, logger); signerErr != nil {
		logger.Fatal("payment-requirements signing is misconfigured", zap.Error(signerErr))
	} else if signer != nil {
		router.Use(signer.Middleware(cfg.RobotID, logger))
	}

	gate, err := mppay.New(cfg, logger)
	if err != nil {
		logger.Error("MPP disabled: failed to build the payment gate", zap.Error(err))
	}
	if gate != nil {
		router.Use(gate.Middleware(x402Middleware))
	} else {
		router.Use(x402Middleware)
	}

	h := handlers.NewHandlers(logger)
	RegisterAllRoutes(router, h)

	// Serve the AIP A2A contract (/.well-known/agent-card.json, /invoke, ...)
	// for any path Gin doesn't own. The gateway proxies these to us verbatim.
	if aipSrv != nil {
		router.NoRoute(gin.WrapH(aipSrv.Handler()))
	}

	return router
}

// newRequirementsSigner builds the payment-requirements signer, or returns
// (nil, nil) when no operator key is configured. A malformed key is fatal rather
// than ignored: silently serving unattested challenges is the failure mode this
// is meant to remove.
func newRequirementsSigner(cfg *config.Config, logger *zap.Logger) (*attest.Signer, error) {
	if cfg.OperatorSigningKey == "" {
		logger.Warn("payment-requirements signing disabled (OPERATOR_SIGNING_KEY not set): " +
			"a payer cannot verify that the advertised recipient came from this operator")
		return nil, nil
	}

	signer, err := attest.NewSigner(cfg.OperatorSigningKey)
	if err != nil {
		return nil, err
	}

	logger.Info("payment-requirements signing enabled",
		zap.String("signer", signer.Address().Hex()),
		zap.String("payee", cfg.EVMPayeeAddress),
	)
	if !strings.EqualFold(signer.Address().Hex(), cfg.EVMPayeeAddress) {
		logger.Warn("payment-requirements signer differs from evm_payee_address; "+
			"payers must be told which address to expect",
			zap.String("signer", signer.Address().Hex()),
			zap.String("payee", cfg.EVMPayeeAddress),
		)
	}

	return signer, nil
}

// RegisterAllRoutes registers all real handlers on the router.
func RegisterAllRoutes(router *gin.Engine, h *handlers.Handlers) {
	router.POST("/action", h.PostAction)
}
