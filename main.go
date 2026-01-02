// Command winres-gen turns an ordinary image into the Windows resources a Go
// program needs to carry an icon and version metadata.
//
// Go's linker automatically picks up a .syso file sitting next to the main
// package, so embedding an icon means generating one file and committing it.
// The GOOS/GOARCH suffix in the name keeps it out of non-Windows builds, and
// because the .syso is a prebuilt artifact, cross-compiling from Linux or macOS
// works with no extra tooling on the build machine.
package main

import (
	"errors"
	"flag"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/josephspurrier/goversioninfo"
)

const usage = `winres-gen - build a Windows .ico and .syso from an image

Usage:
  winres-gen [flags] <image>

Reads a PNG, JPEG or GIF and writes a multi-size .ico plus one .syso resource
object per architecture. Drop the .syso files into a Go main package and the
next build embeds the icon; no build-script changes are needed, including when
cross-compiling.

A source image that is not square is made square first, either by padding it
(nothing lost, artwork smaller) or by cropping it (fills the frame, long edge
trimmed). Pick with -mode.

Flags:
  -mode <pad|crop>     how to square a non-square image (default pad)
  -bg <colour>         pad background: "transparent", "#rgb", "#rrggbb" or
                       "#rrggbbaa" (default transparent; ignored by -mode crop)
  -sizes <list>        icon sizes to generate (default 16,24,32,48,64,128,256)
  -out <dir>           where to write the output (default ".")
  -name <base>         base name for the output files (default: the input's)
  -arch <list>         architectures to emit .syso files for, or "" for none
                       (default amd64,arm64)
  -ico-only            write only the .ico files
  -favicon             also write a web favicon.ico (default true)
  -favicon-name <base> base name for the favicon (default "favicon")
  -favicon-sizes <list>  sizes inside the favicon (default 16,32,48)

Version metadata (all optional; written into the .syso, shown by Windows in the
file's Properties > Details tab):
  -product <text>      product name
  -description <text>  file description - this is what Windows shows most often
  -company <text>      company or author
  -copyright <text>    legal copyright
  -version <x.y.z.b>   file and product version (default 0.0.0.0)
  -exe <name>          original filename, e.g. "edf.exe"

Examples:
  winres-gen -mode pad -out ../myapp logo.png
  winres-gen -mode crop -sizes 16,32,48,256 -exe app.exe -version 1.2.0.0 logo.png
  winres-gen -favicon=false logo.png
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error: "+err.Error())
		os.Exit(1)
	}
}

type config struct {
	mode         fitMode
	bg           string
	sizes        string
	outDir       string
	baseName     string
	arches       string
	icoOnly      bool
	favicon      bool
	faviconName  string
	faviconSizes string
	product      string
	description  string
	company      string
	copyright    string
	version      string
	exeName      string
}

func run(args []string) error {
	var cfg config
	var modeStr string
	var showHelp bool

	fs := flag.NewFlagSet("winres-gen", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	fs.StringVar(&modeStr, "mode", string(modePad), "")
	fs.StringVar(&cfg.bg, "bg", "transparent", "")
	fs.StringVar(&cfg.sizes, "sizes", "16,24,32,48,64,128,256", "")
	fs.StringVar(&cfg.outDir, "out", ".", "")
	fs.StringVar(&cfg.baseName, "name", "", "")
	fs.StringVar(&cfg.arches, "arch", "amd64,arm64", "")
	fs.BoolVar(&cfg.icoOnly, "ico-only", false, "")
	fs.BoolVar(&cfg.favicon, "favicon", true, "")
	fs.StringVar(&cfg.faviconName, "favicon-name", "favicon", "")
	fs.StringVar(&cfg.faviconSizes, "favicon-sizes", "16,32,48", "")
	fs.StringVar(&cfg.product, "product", "", "")
	fs.StringVar(&cfg.description, "description", "", "")
	fs.StringVar(&cfg.company, "company", "", "")
	fs.StringVar(&cfg.copyright, "copyright", "", "")
	fs.StringVar(&cfg.version, "version", "0.0.0.0", "")
	fs.StringVar(&cfg.exeName, "exe", "", "")
	fs.BoolVar(&showHelp, "help", false, "")
	fs.BoolVar(&showHelp, "h", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if showHelp {
		fmt.Print(usage)
		return nil
	}

	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return errors.New("no input image given")
	}
	if len(rest) > 1 {
		return fmt.Errorf("unexpected extra argument %q; one input image at a time", rest[1])
	}
	inPath := rest[0]

	mode, err := parseMode(modeStr)
	if err != nil {
		return err
	}
	bg, err := parseColor(cfg.bg)
	if err != nil {
		return err
	}
	sizes, err := parseSizes(cfg.sizes)
	if err != nil {
		return err
	}
	arches, err := parseArches(cfg.arches)
	if err != nil {
		return err
	}
	var faviconSizes []int
	if cfg.favicon {
		faviconSizes, err = parseSizes(cfg.faviconSizes)
		if err != nil {
			return fmt.Errorf("-favicon-sizes: %w", err)
		}
	}
	if cfg.baseName == "" {
		cfg.baseName = strings.TrimSuffix(filepath.Base(inPath), filepath.Ext(inPath))
	}

	src, err := loadImage(inPath)
	if err != nil {
		return err
	}
	b := src.Bounds()
	if b.Dx() != b.Dy() {
		fmt.Fprintf(os.Stderr, "%s is %dx%d; squaring with -mode %s\n", inPath, b.Dx(), b.Dy(), mode)
	}
	square := makeSquare(src, mode, bg)

	// Upscaling a small source to a big icon only invents detail, so warn.
	if s := square.Bounds().Dx(); s < sizes[len(sizes)-1] {
		fmt.Fprintf(os.Stderr, "warning: source is %dpx square but %dpx was requested; large sizes will be upscaled\n", s, sizes[len(sizes)-1])
	}

	icoBytes, err := renderICO(square, sizes)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(cfg.outDir, 0o755); err != nil {
		return err
	}
	icoPath := filepath.Join(cfg.outDir, cfg.baseName+".ico")
	if err := os.WriteFile(icoPath, icoBytes, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%s)\n", icoPath, sizeList(sizes))

	// The favicon is the same artwork at the handful of sizes a browser asks
	// for; keeping it separate lets the app icon carry 256px detail that a
	// favicon has no use for.
	if cfg.favicon {
		favPath := filepath.Join(cfg.outDir, cfg.faviconName+".ico")
		if favPath == icoPath {
			return fmt.Errorf("favicon would overwrite %s; pass -favicon-name or -name", icoPath)
		}
		favBytes, err := renderICO(square, faviconSizes)
		if err != nil {
			return err
		}
		if err := os.WriteFile(favPath, favBytes, 0o644); err != nil {
			return err
		}
		fmt.Printf("wrote %s (%s)\n", favPath, sizeList(faviconSizes))
	}

	if cfg.icoOnly || len(arches) == 0 {
		return nil
	}
	return writeSyso(cfg, icoPath, arches)
}

// renderICO scales the squared source to each requested size and encodes them
// into one multi-size icon.
func renderICO(square image.Image, sizes []int) ([]byte, error) {
	images := make([]*image.NRGBA, len(sizes))
	for i, s := range sizes {
		images[i] = resize(square, s)
	}
	return encodeICO(images)
}

// writeSyso builds one resource object per architecture. goversioninfo wants a
// path to an .ico on disk, which is why the icon is written out first.
func writeSyso(cfg config, icoPath string, arches []string) error {
	major, minor, patch, build, err := parseVersion(cfg.version)
	if err != nil {
		return err
	}

	exe := cfg.exeName
	if exe == "" {
		exe = cfg.baseName + ".exe"
	}

	vi := &goversioninfo.VersionInfo{
		IconPath: icoPath,
		FixedFileInfo: goversioninfo.FixedFileInfo{
			FileVersion:    goversioninfo.FileVersion{Major: major, Minor: minor, Patch: patch, Build: build},
			ProductVersion: goversioninfo.FileVersion{Major: major, Minor: minor, Patch: patch, Build: build},
			FileFlagsMask:  "3f",
			FileOS:         "040004", // VOS_NT_WINDOWS32
			FileType:       "01",     // VFT_APP
		},
		StringFileInfo: goversioninfo.StringFileInfo{
			ProductName:      cfg.product,
			ProductVersion:   cfg.version,
			FileDescription:  cfg.description,
			FileVersion:      cfg.version,
			CompanyName:      cfg.company,
			LegalCopyright:   cfg.copyright,
			InternalName:     cfg.baseName,
			OriginalFilename: exe,
		},
		VarFileInfo: goversioninfo.VarFileInfo{
			Translation: goversioninfo.Translation{
				LangID:    goversioninfo.LngUSEnglish,
				CharsetID: goversioninfo.CsUnicode,
			},
		},
	}

	for _, arch := range arches {
		vi.Build()
		vi.Walk()
		// The GOOS_GOARCH suffix is a build constraint: the Go toolchain links
		// this object into Windows builds for that architecture only.
		out := filepath.Join(cfg.outDir, fmt.Sprintf("%s_windows_%s.syso", cfg.baseName, arch))
		if err := vi.WriteSyso(out, arch); err != nil {
			return fmt.Errorf("%s: %w", out, err)
		}
		fmt.Printf("wrote %s\n", out)
	}
	return nil
}

func parseSizes(s string) ([]int, error) {
	seen := map[int]bool{}
	var sizes []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("bad size %q", part)
		}
		if n < 1 || n > 256 {
			return nil, fmt.Errorf("size %d out of range; icons run from 1 to 256 pixels", n)
		}
		if !seen[n] {
			seen[n] = true
			sizes = append(sizes, n)
		}
	}
	if len(sizes) == 0 {
		return nil, errors.New("no icon sizes given")
	}
	sort.Ints(sizes)
	return sizes, nil
}

// parseArches accepts the Go architecture names that the resource format knows
// about. An empty list means "no .syso files".
func parseArches(s string) ([]string, error) {
	known := map[string]bool{"386": true, "amd64": true, "arm": true, "arm64": true}
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !known[part] {
			return nil, fmt.Errorf("unsupported architecture %q; choose from 386, amd64, arm, arm64", part)
		}
		out = append(out, part)
	}
	return out, nil
}

func parseVersion(s string) (major, minor, patch, build int, err error) {
	parts := strings.Split(s, ".")
	if len(parts) > 4 {
		return 0, 0, 0, 0, fmt.Errorf("bad version %q; use up to four numbers like 1.2.0.0", s)
	}
	nums := make([]int, 4)
	for i, p := range parts {
		n, convErr := strconv.Atoi(strings.TrimSpace(p))
		if convErr != nil || n < 0 {
			return 0, 0, 0, 0, fmt.Errorf("bad version %q; use up to four numbers like 1.2.0.0", s)
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], nums[3], nil
}

func sizeList(sizes []int) string {
	parts := make([]string, len(sizes))
	for i, s := range sizes {
		parts[i] = strconv.Itoa(s)
	}
	return strings.Join(parts, ", ") + " px"
}
