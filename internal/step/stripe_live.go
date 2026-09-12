package step

import (
	"fmt"

	"github.com/redbotster/nanobots/internal/stripe"
)

// StripeConfig configures direct Stripe access for a service with
// provider: stripe and a non-demo connection. TokenKey defaults to
// "stripe/secret_key" in the same 1Claw vault Google/Slack/GitHub use.
type StripeConfig struct {
	VaultID  string
	TokenKey string
}

func (cfg StripeConfig) tokenConfig() VaultTokenConfig {
	key := cfg.TokenKey
	if key == "" {
		key = "stripe/secret_key"
	}
	return VaultTokenConfig{VaultID: cfg.VaultID, Key: key}
}

// stripeAPI is the subset of *stripe.Client's methods dispatchStripe calls.
type stripeAPI interface {
	InvoicesList(status string, max int) ([]stripe.Invoice, error)
}

func (l *LiveDeps) stripeClient() (stripeAPI, error) {
	cfg := l.Services.Stripe.tokenConfig()
	if !cfg.configured() {
		return nil, fmt.Errorf("stripe: not configured — connect a secret key from Settings")
	}
	if l.stripeTokenCache == nil {
		l.stripeTokenCache = &vaultToken{oc: l.OneClaw, cfg: cfg}
	}
	token, err := l.stripeTokenCache.Get()
	if err != nil {
		return nil, err
	}
	return stripe.NewClient(token), nil
}

func dispatchStripe(c stripeAPI, op string, params map[string]any) (any, error) {
	switch op {
	case "invoices.list":
		status, _ := params["status"].(string)
		invoices, err := c.InvoicesList(status, paramInt(params["max"], 50))
		if err != nil {
			return nil, err
		}
		return toJSONAny(invoices)
	default:
		return nil, fmt.Errorf("stripe: unsupported op %q", op)
	}
}
