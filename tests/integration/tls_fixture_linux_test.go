package integration_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type nativeTestTLS struct {
	root, cert, key string
	roots           *x509.CertPool
	certificate     tls.Certificate
}

// A new test CA signs only this test's loopback leaf; no system trust is changed.
func nativeTLSCertificates(t *testing.T) nativeTestTLS {
	t.Helper()
	directory := t.TempDir()
	now := time.Now()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "isolated Papilio test CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"node.test"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	if err != nil {
		t.Fatal(err)
	}
	rootPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	material := nativeTestTLS{root: filepath.Join(directory, "ca.pem"), cert: filepath.Join(directory, "cert.pem"), key: filepath.Join(directory, "key.pem"), roots: x509.NewCertPool()}
	for path, data := range map[string][]byte{material.root: rootPEM, material.cert: certPEM, material.key: keyPEM} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if !material.roots.AppendCertsFromPEM(rootPEM) {
		t.Fatal("test CA was not loaded")
	}
	material.certificate, err = tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return material
}

// The inner HTTPS exchange verifies its test CA. The target observes receipts;
// a proxy error response is never acceptance.
func secureAccountProbe(t *testing.T, material nativeTestTLS, sockets []string) func(int, bool) {
	t.Helper()
	payload := strings.Repeat("isolated-vision-payload\n", 8192)
	var received atomic.Int64
	destination := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		if r.TLS == nil || r.TLS.Version != tls.VersionTLS13 {
			http.Error(w, "inner TLS1.3 required", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, int64(len(payload)+1)))
		if err != nil || string(body) != payload {
			http.Error(w, "payload mismatch", http.StatusBadRequest)
			return
		}
		w.Write(body)
	}))
	destination.TLS = &tls.Config{Certificates: []tls.Certificate{material.certificate}, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	destination.StartTLS()
	t.Cleanup(destination.Close)
	clients := make([]*http.Client, len(sockets))
	for i, socket := range sockets {
		clients[i] = accountHTTPClient(t, socket)
		clients[i].Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: material.roots, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13}
	}
	return func(index int, allowed bool) {
		t.Helper()
		before := received.Load()
		response, err := clients[index].Post(destination.URL, "application/octet-stream", strings.NewReader(payload))
		var body []byte
		if err == nil {
			body, err = io.ReadAll(io.LimitReader(response.Body, int64(len(payload)+1)))
			response.Body.Close()
		}
		if allowed {
			if err != nil || string(body) != payload || received.Load() != before+1 {
				t.Fatalf("verified secure traffic denied: client%d: %v", index, err)
			}
		} else if received.Load() != before || string(body) == payload {
			t.Fatalf("rejected secure client%d reached actual target", index)
		}
	}
}
