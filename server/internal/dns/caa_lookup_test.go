package dns

import (
	"encoding/binary"
	"net"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/dns/dnsmessage"
)

func TestParseCAAResource(t *testing.T) {
	t.Parallel()

	rec, err := parseCAAResource([]byte{0, 5, 'i', 's', 's', 'u', 'e', 'p', 'k', 'i', '.', 'g', 'o', 'o', 'g'})
	require.NoError(t, err)
	require.Equal(t, CAA{Flag: 0, Tag: "issue", Value: "pki.goog"}, rec)
}

func TestParseCAAAnswersNXDOMAIN(t *testing.T) {
	t.Parallel()

	msg := make([]byte, dnsHeaderLen)
	msg[3] = uint8(dnsmessage.RCodeNameError)
	_, err := parseCAAAnswers(msg, "missing.example.com")
	require.Error(t, err)
	var dnsErr *net.DNSError
	require.ErrorAs(t, err, &dnsErr)
	require.True(t, dnsErr.IsNotFound)
}

func TestParseCAAAnswersExtractsIssueRecord(t *testing.T) {
	t.Parallel()

	rdata := []byte{0, 5, 'i', 's', 's', 'u', 'e', 'l', 'e', 't', 's', 'e', 'n', 'c', 'r', 'y', 'p', 't', '.', 'o', 'r', 'g'}

	msg := make([]byte, 0, 128)
	header := make([]byte, dnsHeaderLen)
	binary.BigEndian.PutUint16(header[4:6], 1)
	binary.BigEndian.PutUint16(header[6:8], 1)
	msg = append(msg, header...)
	msg = appendDNSName(msg, "example.com")
	msg = binary.BigEndian.AppendUint16(msg, uint16(typeCAA))
	msg = binary.BigEndian.AppendUint16(msg, uint16(dnsmessage.ClassINET))
	msg = appendDNSName(msg, "example.com")
	msg = binary.BigEndian.AppendUint16(msg, uint16(typeCAA))
	msg = binary.BigEndian.AppendUint16(msg, uint16(dnsmessage.ClassINET))
	msg = binary.BigEndian.AppendUint32(msg, 60)
	msg = binary.BigEndian.AppendUint16(msg, uint16(len(rdata)))
	msg = append(msg, rdata...)

	records, err := parseCAAAnswers(msg, "example.com")
	require.NoError(t, err)
	require.Equal(t, []CAA{{Flag: 0, Tag: "issue", Value: "letsencrypt.org"}}, records)
}

func appendDNSName(dst []byte, name string) []byte {
	for _, label := range strings.Split(name, ".") {
		dst = append(dst, byte(len(label)))
		dst = append(dst, label...)
	}
	return append(dst, 0)
}
