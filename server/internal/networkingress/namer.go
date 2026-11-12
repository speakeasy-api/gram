package networkingress

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// Namer names everything a stack creates on a customer's tailnet, one method
// per artifact, and decides which connections' artifacts it owns.
// Implementations may consult external state, so every method takes a context
// and can fail; callers treat a failure as "not owned" and do not proceed.
type Namer interface {
	// Hostname returns the connection hostname to store for one an admin
	// requested. It is the MagicDNS label of the connection's Tailscale
	// Service.
	Hostname(ctx context.Context, requested string) (string, error)

	// OwnsHostname reports whether this stack may define, host or delete the
	// Tailscale Service of the connection with hostname.
	OwnsHostname(ctx context.Context, hostname string) (bool, error)

	// ServiceName names the Tailscale Service of the connection with hostname.
	ServiceName(ctx context.Context, hostname string) (string, error)

	// ServiceComment is shown next to a connection's Tailscale Service in the
	// admin console.
	ServiceComment(ctx context.Context) (string, error)

	// MachineName names the gateway machine hosting slot.
	MachineName(ctx context.Context, slot int32) (string, error)

	// AuthKeyDescription describes the auth key minted for one slot of a
	// connection, as the admin console lists it. Tailscale rejects
	// descriptions with punctuation, so it uses only letters, digits and
	// spaces, within maxAuthKeyDescriptionLength.
	AuthKeyDescription(ctx context.Context, ingressID uuid.UUID, slot int32) (string, error)
}

const (
	// machineNamePrefix starts every gateway machine name. Machine and service
	// names share MagicDNS, so machine names stay distinct from service labels.
	machineNamePrefix = "speakeasy-gw-"

	// productionServiceComment is shown next to production services.
	productionServiceComment = "Speakeasy private MCP ingress"

	// maxAuthKeyDescriptionLength keeps auth key descriptions within the 50
	// characters Tailscale's admin console accepts.
	maxAuthKeyDescriptionLength = 50

	// shortIngressIDLength is how much of a connection id an auth key
	// description shows: enough to find the connection, short enough to fit.
	shortIngressIDLength = 8
)

// shortIngressID is the leading hex digits of a connection id.
func shortIngressID(ingressID uuid.UUID) string {
	return ingressID.String()[:shortIngressIDLength]
}

// ProductionNamer names artifacts as customers see them: hostnames as
// requested, services svc:<hostname>, machines speakeasy-gw-<slot>. It owns
// every hostname.
type ProductionNamer struct{}

var _ Namer = ProductionNamer{}

func (ProductionNamer) Hostname(_ context.Context, requested string) (string, error) {
	return requested, nil
}

func (ProductionNamer) OwnsHostname(context.Context, string) (bool, error) { return true, nil }

func (ProductionNamer) ServiceName(_ context.Context, hostname string) (string, error) {
	return "svc:" + hostname, nil
}

func (ProductionNamer) ServiceComment(context.Context) (string, error) {
	return productionServiceComment, nil
}

func (ProductionNamer) MachineName(_ context.Context, slot int32) (string, error) {
	return fmt.Sprintf("%s%d", machineNamePrefix, slot), nil
}

func (ProductionNamer) AuthKeyDescription(_ context.Context, ingressID uuid.UUID, slot int32) (string, error) {
	return fmt.Sprintf("Speakeasy gateway %s slot %d", shortIngressID(ingressID), slot), nil
}

// LocalNamer names a local development stack's artifacts after its qualifier,
// local-<id>, so on a tailnet production also serves they can be told apart and
// never overwrite, host or delete production's. It owns only hostnames that
// carry the qualifier.
type LocalNamer struct {
	qualifier string
}

var _ Namer = LocalNamer{qualifier: ""}

// devIDPattern accepts the short lowercase id `zero:network-ingress` writes.
// At most 8 characters keeps qualified names short.
var devIDPattern = regexp.MustCompile(`^[a-z0-9]{1,8}$`)

// ErrDevIDOutsideLocal rejects a development id outside local development,
// where it would rename production artifacts.
var ErrDevIDOutsideLocal = errors.New("a network ingress development id is only accepted in the local environment")

// NewLocalNamer returns the namer for a local stack's development id.
func NewLocalNamer(devID string) (LocalNamer, error) {
	if !devIDPattern.MatchString(devID) {
		return LocalNamer{qualifier: ""}, fmt.Errorf("invalid network ingress development id %q: use up to 8 lowercase letters or digits", devID)
	}
	return LocalNamer{qualifier: "local-" + devID}, nil
}

// Hostname prefixes requested with local-<id>- unless it already carries it.
func (n LocalNamer) Hostname(_ context.Context, requested string) (string, error) {
	if n.owns(requested) {
		return requested, nil
	}
	return n.qualifier + "-" + requested, nil
}

func (n LocalNamer) OwnsHostname(_ context.Context, hostname string) (bool, error) {
	return n.owns(hostname), nil
}

// ServiceName refuses a hostname this stack does not own, so no caller can
// name, and then touch, a service production may hold.
func (n LocalNamer) ServiceName(_ context.Context, hostname string) (string, error) {
	if !n.owns(hostname) {
		return "", fmt.Errorf("hostname %q is not owned by local development stack %s", hostname, n.qualifier)
	}
	return "svc:" + hostname, nil
}

func (n LocalNamer) ServiceComment(context.Context) (string, error) {
	return fmt.Sprintf("Speakeasy local development (%s)", n.qualifier), nil
}

func (n LocalNamer) MachineName(_ context.Context, slot int32) (string, error) {
	return fmt.Sprintf("%s%s-%d", machineNamePrefix, n.qualifier, slot), nil
}

// AuthKeyDescription names the stack as "local <id>": Tailscale rejects the
// hyphen in its qualifier.
func (n LocalNamer) AuthKeyDescription(_ context.Context, ingressID uuid.UUID, slot int32) (string, error) {
	return fmt.Sprintf("Speakeasy local %s %s slot %d", strings.TrimPrefix(n.qualifier, "local-"), shortIngressID(ingressID), slot), nil
}

func (n LocalNamer) owns(hostname string) bool {
	return strings.HasPrefix(hostname, n.qualifier+"-")
}

// NewNamer selects the namer for a process: a LocalNamer when a development
// id is configured, which only local development accepts, and the
// ProductionNamer otherwise.
func NewNamer(environment, devID string) (Namer, error) {
	if devID == "" {
		return ProductionNamer{}, nil
	}
	if environment != "local" {
		return nil, ErrDevIDOutsideLocal
	}
	return NewLocalNamer(devID)
}
