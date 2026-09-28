// Command devnode boots a throwaway in-memory gno chain and prints where it is.
//
// It exists so scripts/screenshots.sh can capture REAL gnopie output without a
// network and without a funded account on a real chain. It is the same node the
// test suite runs against, out of the same integration package, so a capture
// taken here is a capture of the code paths the tests cover.
//
// Not a development tool: `gnodev` is that, and it is better at it. This one
// boots, prints one line, and waits to be killed.
//
//	go run ./scripts/devnode -pkgs gno.land/r/demo/counter -home /tmp/h
//	# -> rpc=http://127.0.0.1:NNNNN chainid=tendermint_test key=test1
//
// With -home it also writes a GNOHOME that already believes gno.land IS this
// node: a pre-warmed discovery cache, a config naming the funded test key, and a
// keybase holding it. That is what lets a capture show the real command line,
// `gnopie gno.land/r/demo/counter`, instead of a pile of test flags.
//
// GNOROOT has to point at a gnolang/gno checkout at the version go.mod pins, for
// the same reason the tests do: the realms are loaded out of its examples/ and
// type-checked by the linked VM.
package main

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/gnolang/gno/gno.land/pkg/gnoland"
	"github.com/gnolang/gno/gno.land/pkg/gnoland/ugnot"
	"github.com/gnolang/gno/gno.land/pkg/integration"
	"github.com/gnolang/gno/gnovm/pkg/gnoenv"
	"github.com/gnolang/gno/tm2/pkg/crypto/keys"
	"github.com/gnolang/gno/tm2/pkg/log"
	"github.com/gnolang/gno/tm2/pkg/std"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "devnode:", err)
		os.Exit(1)
	}
}

func run() error {
	pkgs := flag.String("pkgs", "gno.land/r/demo/counter",
		"comma-separated realm paths to load into genesis, relative to GNOROOT/examples")
	home := flag.String("home", "",
		"if set, write a GNOHOME here pointing gno.land at this node, with the test key in it")
	flag.Parse()

	rootdir := gnoenv.RootDir()
	examples := filepath.Join(rootdir, "examples")
	if _, err := os.Stat(examples); err != nil {
		return fmt.Errorf("GNOROOT=%q has no examples/.\n"+
			"Point it at a gnolang/gno checkout at the tag go.mod pins (see README, Testing)", rootdir)
	}

	config := integration.TestingMinimalNodeConfig(rootdir)
	paths := splitNonEmpty(*pkgs)
	if len(paths) > 0 {
		txs, err := loadPkgs(examples, paths)
		if err != nil {
			return err
		}
		state := config.Genesis.AppState.(gnoland.GnoGenesisState)
		state.Txs = append(state.Txs, txs...)
		config.Genesis.AppState = state
	}

	node, addr := integration.TestingInMemoryNode(fatal{}, log.NewNoopLogger(), config)
	defer func() { _ = node.Stop() }()

	if *home != "" {
		if err := seedHome(*home, addr); err != nil {
			return err
		}
	}

	// One line, parseable, on stdout. Everything else goes to stderr so a caller
	// can read this with `head -1` without a delimiter dance.
	fmt.Printf("rpc=%s chainid=%s key=%s address=%s\n",
		addr, "tendermint_test", integration.DefaultAccount_Name, integration.DefaultAccount_Address)
	fmt.Fprintln(os.Stderr, "devnode ready, ctrl-c to stop")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	return nil
}

func loadPkgs(examples string, paths []string) ([]gnoland.TxWithMetadata, error) {
	loader := integration.NewPkgsLoader()
	for _, p := range paths {
		if err := loader.LoadPackage(examples, filepath.Join(examples, filepath.Clean(p)), ""); err != nil {
			return nil, fmt.Errorf("loading %s: %w\n"+
				"If this is a type error, GNOROOT is at a different version than go.mod pins", p, err)
		}
	}
	privKey, err := integration.GeneratePrivKeyFromMnemonic(integration.DefaultAccount_Seed, "", 0, 0)
	if err != nil {
		return nil, err
	}
	return loader.GenerateTxs(privKey, std.NewFee(50000, std.MustParseCoin(ugnot.ValueString(1000000))), nil)
}

// seedHome writes the discovery cache, the config and the keybase a gnopie run
// against this node needs. The cache entry is what stops discovery reaching out
// to the real gno.land over the network.
func seedHome(home, rpc string) error {
	if err := os.MkdirAll(filepath.Join(home, "gnopie", "cache"), 0o755); err != nil {
		return err
	}
	// cachePath's layout, kept in step with discover.go: sha256 of the domain,
	// first 8 bytes, hex.
	sum := sha256.Sum256([]byte("gno.land"))
	cacheFile := filepath.Join(home, "gnopie", "cache", fmt.Sprintf("%x.toml", sum[:8]))
	body := fmt.Sprintf("cached_at = 2099-01-01T00:00:00Z\nchain_id = %q\nname = \"gno.land\"\nrpc = %q\n",
		"tendermint_test", rpc)
	if err := os.WriteFile(cacheFile, []byte(body), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(home, "gnopie", "config.toml"),
		[]byte(fmt.Sprintf("key = %q\n", integration.DefaultAccount_Name)), 0o644); err != nil {
		return err
	}
	kb, err := keys.NewKeyBaseFromDir(home)
	if err != nil {
		return err
	}
	if _, err := kb.CreateAccount(
		integration.DefaultAccount_Name, integration.DefaultAccount_Seed, "", "", 0, 0); err != nil {
		return fmt.Errorf("creating the test key: %w", err)
	}
	return nil
}

func splitNonEmpty(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// fatal satisfies the {Errorf, FailNow} the integration helpers want. There is no
// test here, so a failure is the program's failure.
type fatal struct{}

func (fatal) Errorf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "devnode: "+format+"\n", args...)
}
func (fatal) FailNow() { os.Exit(1) }
