package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"text/tabwriter"
)

// sdmFile is the root file of the IDE plug-ins in the folder of go.mod: their tool window shows its
// lines. sdmc takes the configurations from it: the lines <dir>/go.dal, <dir>/python.dal, <dir>/cpp.dal.
const sdmFile = ".sdm"

// stdin and stdinIsTerminal serve the choice of a configuration in the console; tests replace them.
var stdin io.Reader = os.Stdin

var stdinIsTerminal = func() bool { return isTerminal(os.Stdin) }

// dalFiles returns the paths <dir>/go.dal, python.dal and cpp.dal of .sdm in their order. "# comment",
// "--- separator" and the other files are skipped; "path|title" is path.
func dalFiles(text string) []string {
	var res []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "---") {
			continue
		}
		p, _, _ := strings.Cut(line, "|")
		p = cleanRel(strings.TrimSpace(p))
		if b := path.Base(p); b == "go.dal" || b == "python.dal" || b == "cpp.dal" {
			res = append(res, p)
		}
	}
	return res
}

// configFlag adds -c to a command that reads a configuration: a number of .sdm or "all".
func configFlag(fs *flag.FlagSet) *string {
	return fs.String("c", "", "number of the configuration (go.dal, python.dal, cpp.dal) in "+sdmFile+", or all")
}

// allConfigs is the value of -c for every configuration of .sdm, one after another.
const allConfigs = "all"

// errNotChosen: the table of configurations is already printed, nothing to add.
var errNotChosen = errors.New("no configuration chosen")

// config is a folder of sdm.xml and settings.xml; label is "N:<dir>/go.dal" when it comes from .sdm.
type config struct{ label, dir string }

// configDirs are the configurations to run: -dir, if given; otherwise from .sdm: -c N, -c all, the
// only go.dal, or the choice in the console (a number or all). Without go.dal in .sdm (or without
// .sdm) it is -dir by default. When several are left to choose from, it prints them with the
// command for each: cmdline is the command as typed (sdmc ls -l), -c N is added to it.
func configDirs(fs *flag.FlagSet, dir, c, cmdline string, stderr io.Writer) ([]config, error) {
	dirSet := false
	fs.Visit(func(f *flag.Flag) { dirSet = dirSet || f.Name == "dir" })
	if dirSet && c != "" {
		return nil, errors.New("-dir and -c: one of them")
	}
	if dirSet {
		return []config{{dir: cleanRel(dir)}}, nil
	}
	text, err := os.ReadFile(sdmFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	dals := dalFiles(string(text))
	switch {
	case len(dals) == 0 && c != "":
		return nil, fmt.Errorf("-c %s: no <dir>/go.dal, <dir>/python.dal or <dir>/cpp.dal in %s", c, sdmFile)
	case len(dals) == 0:
		return []config{{dir: cleanRel(dir)}}, nil
	case c != "":
		if res, ok := pick(dals, c); ok {
			return res, nil
		}
		fmt.Fprintf(stderr, "-c %s: %s has %d configurations:\n\n", c, sdmFile, len(dals))
		printConfigs(stderr, dals, cmdline)
		return nil, errNotChosen
	case len(dals) == 1:
		return pick1(dals, 1), nil
	}
	// stderr: stdout may go to a file (sdmc xml > models.xml)
	fmt.Fprintf(stderr, "%s has %d configurations:\n\n", sdmFile, len(dals))
	printConfigs(stderr, dals, cmdline)
	if !stdinIsTerminal() {
		return nil, errNotChosen
	}
	in := bufio.NewReader(stdin)
	for {
		fmt.Fprintf(stderr, "\nconfiguration 1..%d or %s (Enter: cancel): ", len(dals), allConfigs)
		line, err := in.ReadString('\n')
		s := strings.TrimSpace(line)
		if s == "" {
			if err != nil {
				fmt.Fprintln(stderr) // EOF: the prompt has no line break
			}
			return nil, errNotChosen
		}
		if res, ok := pick(dals, s); ok {
			return res, nil
		}
		fmt.Fprintf(stderr, "%q: a number from the table, 1..%d, or %s\n", s, len(dals), allConfigs)
		if err != nil {
			return nil, errNotChosen
		}
	}
}

// pick: "all" or the number of a configuration of .sdm.
func pick(dals []string, s string) ([]config, bool) {
	if strings.EqualFold(s, allConfigs) {
		var res []config
		for i := range dals {
			res = append(res, pick1(dals, i+1)...)
		}
		return res, true
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= len(dals) {
		return pick1(dals, n), true
	}
	return nil, false
}

func pick1(dals []string, n int) []config {
	return []config{{label: fmt.Sprintf("%d:%s", n, dals[n-1]), dir: path.Dir(dals[n-1])}}
}

// printConfigs is the table: number, go.dal of .sdm, the command to run for it; all of them at the end.
func printConfigs(w io.Writer, dals []string, cmdline string) {
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "  #\tconfiguration\tcommand")
	for i, d := range dals {
		fmt.Fprintf(tw, "  %d\t%s\t%s -c %d\n", i+1, d, cmdline, i+1)
	}
	fmt.Fprintf(tw, "  %s\t(one after another)\t%s -c %s\n", allConfigs, cmdline, allConfigs)
	_ = tw.Flush()
}

// commandLine is the command as typed, without -c: "sdmc ls -l"; an argument with a space is quoted.
func commandLine(cmd string, args []string) string {
	parts := []string{"sdmc", cmd}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-c" || a == "--c" {
			i++ // and its value
			continue
		}
		if strings.HasPrefix(a, "-c=") || strings.HasPrefix(a, "--c=") {
			continue
		}
		if strings.ContainsAny(a, " \t") {
			a = strconv.Quote(a)
		}
		parts = append(parts, a)
	}
	return strings.Join(parts, " ")
}

// checkConfig: the folder of a configuration of .sdm is the folder of its go.dal line, settings.xml
// has to be there. Without it the error names the line of .sdm, not only the missing file.
func checkConfig(cfg config) error {
	if cfg.label == "" {
		return nil // -dir or the default: the error of the command is clear enough
	}
	if _, err := os.Stat(path.Join(cfg.dir, "settings.xml")); err == nil || !errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("%s in %s: no settings.xml in %s; fix the line (<folder of settings.xml>/%s) or run sdmc init with -dir %s",
		cfg.label, sdmFile, cfg.dir, path.Base(cfg.label), cfg.dir)
}

// forEachConfig runs a command for each of its configurations (configDirs): several get a header
// "--- N:<dir>/go.dal" each, they go on after a failure; the exit code is the worst of them.
func forEachConfig(fs *flag.FlagSet, dir, c, cmd string, args []string, stdout, stderr io.Writer, run func(sdmDir string) int) int {
	cfgs, err := configDirs(fs, dir, c, commandLine(cmd, args), stderr)
	if errors.Is(err, errNotChosen) {
		return 2
	}
	if err != nil {
		printErr(stderr, err)
		return 2
	}
	worst := 0
	for i, cfg := range cfgs {
		if len(cfgs) > 1 {
			if i > 0 {
				fmt.Fprintln(stdout)
			}
			fmt.Fprintln(stdout, "--- "+cfg.label)
		}
		if err := checkConfig(cfg); err != nil {
			printErr(stderr, err)
			worst = max(worst, 1)
			continue
		}
		worst = max(worst, run(cfg.dir))
	}
	return worst
}
