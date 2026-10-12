// The offline trust tool creates distinct role keys and a quorum-signed root.
package main

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sigstore/sigstore/pkg/cryptoutils"
	"github.com/sigstore/sigstore/pkg/signature"
	"github.com/theupdateframework/go-tuf/v2/metadata"

	"lumen/internal/update"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	keys := flag.String("keys", "", "new absolute private-key directory outside the checkout")
	out := flag.String("out", "", "new public root metadata file")
	previous := flag.String("previous-root", "", "previous signed root for rotation")
	oldKeys := flag.String("previous-keys", "", "external previous root quorum PEM files, comma-separated")
	flag.Parse()
	if *keys == "" || *out == "" || !filepath.IsAbs(*keys) {
		return errors.New("an absolute external key directory and public output file are required")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(cwd, *keys)
	if err == nil && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
		return errors.New("private keys must remain outside the working checkout")
	}
	if _, err := os.Stat(*out); !errors.Is(err, os.ErrNotExist) {
		return errors.New("public root output must not already exist")
	}
	root := metadata.Root(time.Now().UTC().Add(365 * 24 * time.Hour))
	root.Signed.ConsistentSnapshot = true
	var oldRoot *metadata.Metadata[metadata.RootType]
	if *previous != "" {
		data, err := os.ReadFile(*previous)
		if err != nil {
			return err
		}
		oldRoot, err = metadata.Root().FromBytes(data)
		if err != nil {
			return err
		}
		if err := oldRoot.VerifyDelegate(metadata.ROOT, oldRoot); err != nil {
			return err
		}
		root.Signed.Version = oldRoot.Signed.Version + 1
		if *oldKeys == "" {
			return errors.New("root rotation requires the previous offline quorum")
		}
	}
	if err := os.Mkdir(*keys, 0o700); err != nil {
		return err
	}
	if err := update.ProtectSecrets(*keys); err != nil {
		return err
	}
	var rootSigners []signature.Signer
	for _, role := range []string{metadata.ROOT, metadata.TARGETS, metadata.SNAPSHOT, metadata.TIMESTAMP} {
		count, threshold := 1, 1
		if role == metadata.ROOT || role == metadata.TARGETS {
			count, threshold = 3, 2
		}
		root.Signed.Roles[role].Threshold = threshold
		for i := range count {
			public, private, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				return err
			}
			key, err := metadata.KeyFromPublicKey(public)
			if err != nil {
				return err
			}
			if err := root.Signed.AddKey(key, role); err != nil {
				return err
			}
			der, err := x509.MarshalPKCS8PrivateKey(private)
			if err != nil {
				return err
			}
			file := filepath.Join(*keys, role+"-"+strconv.Itoa(i+1)+".pem")
			f, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return err
			}
			_, writeErr := f.Write(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
			syncErr := f.Sync()
			closeErr := f.Close()
			if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
				return err
			}
			if role == metadata.ROOT && i < threshold {
				signer, err := signature.LoadSigner(private, crypto.Hash(0))
				if err != nil {
					return err
				}
				rootSigners = append(rootSigners, signer)
			}
		}
	}
	for _, signer := range rootSigners {
		if _, err := root.Sign(signer); err != nil {
			return err
		}
	}
	if oldRoot != nil {
		for _, file := range strings.Split(*oldKeys, ",") {
			data, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			private, err := cryptoutils.UnmarshalPEMToPrivateKey(data, nil)
			if err != nil {
				return err
			}
			signer, err := signature.LoadSigner(private, crypto.Hash(0))
			if err != nil {
				return err
			}
			if _, err := root.Sign(signer); err != nil {
				return err
			}
		}
		if err := oldRoot.VerifyDelegate(metadata.ROOT, root); err != nil {
			return err
		}
	}
	if err := root.VerifyDelegate(metadata.ROOT, root); err != nil {
		return err
	}
	data, err := root.ToBytes(false)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(*out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if _, err := file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}
