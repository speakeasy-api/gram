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
	// spaces, within the 50 characters it accepts.
	AuthKeyDescription(ctx context.Context, ingressID uuid.UUID, slot int32) (string, error)
}

const (
	// machineNamePrefix starts every gateway machine name. Machine and service
	// names share MagicDNS, so machine names stay distinct from service labels.
	machineNamePrefix = "speakeasy-gw-"

	// productionServiceComment is shown next to production services.
	productionServiceComment = "Speakeasy private MCP ingress"

	// shortIngressIDLength is how much of a connection id an auth key
	// description shows: enough to find the connection, short enough to fit.
	shortIngressIDLength = 8
)

// shortIngressID is the leading hex digits of a connection id.
func shortIngressID(ingressID uuid.UUID) string {
	return ingressID.String()[:shortIngressIDLength]
}

// localHostnamePrefix starts every hostname a local development stack
// creates. Production reserves it, so a production connection can never
// manage a service a local stack on the same tailnet also manages.
const localHostnamePrefix = "local-"

// ErrReservedHostname rejects a requested hostname reserved for local
// development stacks.
var ErrReservedHostname = errors.New(`hostnames starting with "local-" are reserved`)

// ProductionNamer names artifacts as customers see them: hostnames as
// requested, services svc:<hostname>, machines speakeasy-gw-<slot>. It owns
// every hostname except the local- ones it reserves for local development.
type ProductionNamer struct{}

var _ Namer = ProductionNamer{}

func (ProductionNamer) Hostname(_ context.Context, requested string) (string, error) {
	if strings.HasPrefix(requested, localHostnamePrefix) {
		return "", ErrReservedHostname
	}
	return requested, nil
}

func (ProductionNamer) OwnsHostname(_ context.Context, hostname string) (bool, error) {
	return !strings.HasPrefix(hostname, localHostnamePrefix), nil
}

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
	return LocalNamer{qualifier: localHostnamePrefix + devID}, nil
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

// QualifiedNamer names a deployed environment's gateway machines after its
// qualifier, speakeasy-gw-<qualifier>-<slot>, so two environments serving one
// tailnet, such as dev beside production, register devices that can be told
// apart. Hostnames, services and ownership are production's: the qualifier
// renames only what each environment registers for itself.
type QualifiedNamer struct {
	ProductionNamer

	qualifier string
}

var _ Namer = QualifiedNamer{ProductionNamer: ProductionNamer{}, qualifier: ""}

// machineNameQualifierPattern accepts a short lowercase qualifier such as dev.
// At most 8 characters keeps machine names short, and without punctuation the
// qualifier fits auth key descriptions, which Tailscale limits to letters,
// digits and spaces.
var machineNameQualifierPattern = regexp.MustCompile(`^[a-z0-9]{1,8}$`)

// ErrQualifierInLocal rejects a machine name qualifier in local development:
// a local stack qualifies every name, hostnames included, with its
// development id, and a qualifier would leave its hostnames production's.
var ErrQualifierInLocal = errors.New("a gateway machine name qualifier is not accepted in local development; use the network ingress development id")

// NewQualifiedNamer returns the namer for an environment's machine name
// qualifier. Qualifiers starting with local are refused so an environment's
// machines never look like a local stack's.
func NewQualifiedNamer(qualifier string) (QualifiedNamer, error) {
	if !machineNameQualifierPattern.MatchString(qualifier) || strings.HasPrefix(qualifier, "local") {
		return QualifiedNamer{ProductionNamer: ProductionNamer{}, qualifier: ""}, fmt.Errorf("invalid gateway machine name qualifier %q: use up to 8 lowercase letters or digits, not starting with local", qualifier)
	}
	return QualifiedNamer{ProductionNamer: ProductionNamer{}, qualifier: qualifier}, nil
}

func (n QualifiedNamer) ServiceComment(context.Context) (string, error) {
	return fmt.Sprintf("%s (%s)", productionServiceComment, n.qualifier), nil
}

func (n QualifiedNamer) MachineName(_ context.Context, slot int32) (string, error) {
	return fmt.Sprintf("%s%s-%d", machineNamePrefix, n.qualifier, slot), nil
}

func (n QualifiedNamer) AuthKeyDescription(_ context.Context, ingressID uuid.UUID, slot int32) (string, error) {
	return fmt.Sprintf("Speakeasy gateway %s %s slot %d", n.qualifier, shortIngressID(ingressID), slot), nil
}

// NewNamer selects the namer for a process: a LocalNamer when a development
// id is configured, which only local development accepts; a QualifiedNamer
// when a gateway machine name qualifier is, which only a deployed environment
// accepts; and the ProductionNamer otherwise.
func NewNamer(environment, devID, qualifier string) (Namer, error) {
	switch {
	case qualifier != "" && (devID != "" || environment == "local"):
		return nil, ErrQualifierInLocal
	case devID != "":
		if environment != "local" {
			return nil, ErrDevIDOutsideLocal
		}
		return NewLocalNamer(devID)
	case qualifier != "":
		return NewQualifiedNamer(qualifier)
	default:
		return ProductionNamer{}, nil
	}
}
