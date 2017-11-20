package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

//-----------------------------------------------------------------------------

func syncFile(src, dst string, typeRename ...string) error {
	typePairs, err := validateFormat(typeRename...)
	if err != nil {
		return err
	}

	src, _ = filepath.Abs(src)
	dst, _ = filepath.Abs(dst)
	if err := fileExists(src); err != nil {
		return errors.WithMessage(err, "source file")
	}

	// filter based on typeRename
	var original map[string]string
	if err := fileExists(dst); err == nil {
		buffer, err := originalTypedefs(dst)
		if err != nil {
			logerr.Println(err)
		}
		nameset := make(map[string]struct{})
		for _, v := range typeRename {
			nameset[v] = struct{}{}
		}
		original = make(map[string]string)
		for k, v := range buffer {
			if _, ok := nameset[k]; ok {
				continue
			}
			nameset[k] = struct{}{}
			original[k] = v
		}
		if len(original) == 0 {
			original = nil
		}
	}

	if err := adoptPackage(src, dst); err != nil {
		return err
	}

	renameTypes(dst, typePairs)

	// redefine types
	if original != nil {
		fileset, typeset, err := findTypes(dst)
		if err != nil {
			return err
		}

		var list []typeEntry
		for _, vt := range typeset {
			start := int(fileset.Position(vt.Pos()).Offset)
			end := int(fileset.Position(vt.End()).Offset)
			list = append(list, typeEntry{
				startOffset: start,
				endOffset:   end,
				spec:        vt,
			})
		}

		sort.Sort(entries(list))

		lastPos := 0
		content, err := ioutil.ReadFile(dst)
		if err != nil {
			return err
		}
		to := &bytes.Buffer{}
		for _, ve := range list {
			oldDef, ok := original[ve.spec.Name.Name]
			if !ok || len(oldDef) == 0 {
				continue
			}
			to.Write(content[lastPos:ve.startOffset])
			to.Write([]byte(oldDef))
			lastPos = ve.endOffset
		}
		if lastPos > 0 {
			to.Write(content[lastPos:])
		}
		if err := writeFile(dst, string(to.Bytes())); err != nil {
			return err
		}
	}

	return nil
}

type typeEntry struct {
	startOffset, endOffset int
	spec                   *ast.TypeSpec
}

type entries []typeEntry

func (e entries) Len() int           { return len(e) }
func (e entries) Swap(i, j int)      { e[i], e[j] = e[j], e[i] }
func (e entries) Less(i, j int) bool { return e[i].startOffset < e[j].startOffset }

func originalTypedefs(fp string) (map[string]string, error) {
	originalFileset, originalTypeset, err := findTypes(fp)
	if err != nil {
		return nil, err
	}
	content, err := ioutil.ReadFile(fp)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	for _, vt := range originalTypeset {
		start := int(originalFileset.Position(vt.Pos()).Offset)
		end := int(originalFileset.Position(vt.End()).Offset)
		result[vt.Name.Name] = fmt.Sprintf("%s", content[start:end])
	}
	return result, nil
}

func renameTypes(dst string, typePairs [][2]string) error {
	fileset, typeset, err := findTypes(dst)
	if err != nil {
		return err
	}
	_, _ = fileset, typeset

	var positions []pos
	for _, pair := range typePairs {
		for _, vt := range typeset {
			if vt.Name.Name != pair[1] {
				continue
			}
			start := int(fileset.Position(vt.Name.Pos()).Offset)
			outset, errset, err := guru(dst, importPath(dst), start)
			if err != nil {
				return err
			}
			if len(errset) > 0 {
				logerr.Printf("%s", errset)
				continue
			}

			spanset := extractPositions(outset)
			spanset = filterSpans(dst, spanset)
			positions = append(positions, pos{spanset, pair})
		}
	}
	replace(dst, positions)
	return nil
}

type pos struct {
	spans []span
	names [2]string
}

func replace(fp string, positions []pos) {
	content, err := ioutil.ReadFile(fp)
	if err != nil {
		logerr.Println(err)
		return
	}
	from := bytes.NewBuffer(content)
	var lineset [][]byte
	for err == nil {
		var line []byte
		line, err = from.ReadBytes('\n')
		if err != nil {
			break
		}
		lineset = append(lineset, line)
	}
	to := &bytes.Buffer{}
	for kline, vline := range lineset {
		written := false

		var lineSpans []span
		names := make(map[span][2]string)
		for _, vp := range positions {
			for _, vs := range vp.spans {
				// code assumes startline and the end line are the same
				if vs.startline != vs.endline {
					logerr.Printf("broken assumption %v", vs)
					continue
				}
				if vs.startline != kline+1 {
					continue
				}
				lineSpans = append(lineSpans, vs)
				names[vs] = vp.names
			}
		}

		sort.Sort(sortedSpans(lineSpans))
		lastPos := 0
		for _, vs := range lineSpans {
			to.Write(vline[lastPos : vs.startpos-1])
			to.Write([]byte(names[vs][0]))
			lastPos = vs.endpos
			written = true
		}
		if lastPos > 0 {
			to.Write(vline[lastPos:])
		}

		if written {
			continue
		}
		to.Write(vline)
	}
	if err := writeFile(fp, string(to.Bytes())); err != nil {
		logerr.Println(err)
	}
}

type sortedSpans []span

func (ss sortedSpans) Len() int           { return len(ss) }
func (ss sortedSpans) Swap(i, j int)      { ss[i], ss[j] = ss[j], ss[i] }
func (ss sortedSpans) Less(i, j int) bool { return ss[i].startpos < ss[j].startpos }

func filterSpans(fp string, spans []span) []span {
	var result []span
	for _, vs := range spans {
		if vs.filepath != fp {
			continue
		}
		result = append(result, vs)
	}
	return result
}

func extractPositions(outset []byte) []span {
	var (
		rg     = regexp.MustCompile("(?P<filepath>[^:]+):(?P<startline>\\d+)\\.(?P<startpos>\\d+)-(?P<endline>\\d+)\\.(?P<endpos>\\d+):")
		nl     = rg.SubexpNames()
		buf    = bytes.NewBuffer(outset)
		line   string
		err    error
		result []span
	)
	for line, err = "", error(nil); err == nil; line, err = buf.ReadString('\n') {
		if line == "" {
			continue
		}
		matchset := rg.FindAllStringSubmatch(line, -1)
		if len(matchset) == 0 {
			continue
		}
		parts := make(map[string]string)
		for k, v := range matchset[0] {
			if k == 0 {
				continue
			}
			parts[nl[k]] = v
		}
		startline, err := strconv.Atoi(parts["startline"])
		if err != nil {
			logerr.Println(err)
			continue
		}
		startpos, err := strconv.Atoi(parts["startpos"])
		if err != nil {
			logerr.Println(err)
			continue
		}
		endline, err := strconv.Atoi(parts["endline"])
		if err != nil {
			logerr.Println(err)
			continue
		}
		endpos, err := strconv.Atoi(parts["endpos"])
		if err != nil {
			logerr.Println(err)
			continue
		}
		fpath := parts["filepath"]

		result = append(result, span{
			startline: startline,
			startpos:  startpos,
			endline:   endline,
			endpos:    endpos,
			filepath:  fpath,
		})
	}
	if err != nil && err != io.EOF {
		logerr.Println(err)
	}
	return result
}

type span struct {
	startline, startpos, endline, endpos int
	filepath                             string
}

func adoptPackage(src, dst string) error {
	pkg, _, _, err := getPackage(dst)
	if err != nil {
		return err
	}
	if err := cp(src, dst, true); err != nil {
		return err
	}
	_, pos, end, err := getPackage(dst)
	if err != nil {
		return err
	}
	content, err := ioutil.ReadFile(dst)
	if strings.HasPrefix(pkg, "package") {
		pkg = strings.TrimPrefix(pkg, "package")
	}
	pkg = strings.TrimSpace(pkg)
	return writeFile(
		dst,
		fmt.Sprintf("%s%s%s", content[:pos], "package "+pkg, content[end:]))
}

func findTypes(fp string) (*token.FileSet, []*ast.TypeSpec, error) {
	fset := token.NewFileSet()
	fast, err := parser.ParseFile(
		fset,
		fp,
		nil,
		parser.AllErrors)
	if err != nil {
		return nil, nil, err
	}
	var res []*ast.TypeSpec
	for _, vdec := range fast.Decls {
		x, ok := vdec.(*ast.GenDecl)
		if !ok {
			continue
		}
		if !x.Tok.IsKeyword() {
			continue
		}
		if x.Tok.String() != "type" {
			continue
		}
		if len(x.Specs) == 0 {
			continue
		}
		spec, ok := x.Specs[0].(*ast.TypeSpec)
		if !ok {
			continue
		}
		// if spec.Assign == 0 {
		// 	continue
		// }
		res = append(res, spec)
	}

	return fset, res, nil
}

func importPath(path string) string {
	src, err := checkSrcDir()
	if err != nil {
		panic(err)
	}
	dir := path
	if err := dirExists(dir); err != nil {
		if err == errNotDir {
			dir = filepath.Dir(dir)
		} else {
			panic(err)
		}
	}
	return strings.Trim(strings.Replace(dir, src, "", -1), string([]rune{filepath.Separator}))
}

func validateFormat(typeRename ...string) ([][2]string, error) {
	r := regexp.MustCompile("^(?P<new>\\w+)=(?P<old>\\w+)$")
	nl := r.SubexpNames()
	var res [][2]string
	for _, v := range typeRename {
		rr := r.FindAllStringSubmatch(v, -1)
		if len(rr) == 0 {
			return nil, errorf("invalid type rename: %v", v)
		}
		parts := make(map[string]string)
		for k, v := range rr[0] {
			parts[nl[k]] = v
		}
		var pair [2]string
		pair[0] = parts["new"]
		pair[1] = parts["old"]
		res = append(res, pair)
	}
	return res, nil
}

//-----------------------------------------------------------------------------

func guru(fp, pip string, start int) ([]byte, []byte, error) {
	cmd := exec.Command(
		"guru",
		"-scope", pip,
		"referrers", fmt.Sprintf("%v:#%v", fp, start))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	defer stdout.Close()

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, err
	}
	defer stderr.Close()

	errset := &bytes.Buffer{}
	outset := &bytes.Buffer{}
	go func() { io.Copy(outset, stdout) }()
	go func() { io.Copy(errset, stderr) }()

	if err := cmd.Start(); err != nil {
		logerr.Println(err)
	}
	if err := cmd.Wait(); err != nil {
		return nil, nil, err
	}
	return outset.Bytes(), errset.Bytes(), nil
}

//-----------------------------------------------------------------------------
// source code

func getPackage(fp string) (string, int, int, error) {
	pos, end, err := filePkg(fp, true)
	if err == nil {
		content, err := ioutil.ReadFile(fp)
		if err == nil {
			return fmt.Sprintf("%s", content[pos:end]), pos, end, nil
		}
	}
	pos, end, err = filePkg(fp)
	if err == nil {
		content, err := ioutil.ReadFile(fp)
		if err == nil {
			return fmt.Sprintf("%s", content[pos:end]), pos, end, nil
		}
	}
	dir := filepath.Dir(fp)
	if err := dirExists(dir); err != nil {
		return "", 0, 0, err
	}
	return filepath.Base(dir), 0, 0, nil
}

func filePkg(fp string, exclude ...bool) (int, int, error) {
	x := false
	if len(exclude) > 0 {
		x = exclude[0]
	}

	if x {
		dir := filepath.Dir(fp)
		list, err := ioutil.ReadDir(dir)
		if err != nil {
			return 0, 0, err
		}
		for _, v := range list {
			if v.IsDir() {
				continue
			}
			pos, end, err := filePkg(filepath.Join(dir, v.Name()))
			if err == nil {
				return pos, end, nil
			}
		}
		return 0, 0, errNoPackage
	}

	fset := token.NewFileSet()
	fast, err := parser.ParseFile(fset, fp, nil, parser.PackageClauseOnly)
	if err != nil {
		return 0, 0, errors.WithStack(err)
	}

	pos := int(fset.Position(fast.Pos()).Offset)
	end := int(fset.Position(fast.End()).Offset)

	return pos, end, nil
}

var errNoPackage = errorf("NO PKG FOUND")

//-----------------------------------------------------------------------------
// file system

func checkSrcDir() (string, error) {
	gopath := os.Getenv("GOPATH")
	if err := dirExists(gopath); err != nil {
		return "", errors.WithMessage(err, fmt.Sprintf("not found, $GOPATH = %v", gopath))
	}

	parts := strings.Split(gopath, string([]rune{filepath.ListSeparator}))
	gopath = parts[0]

	src := filepath.Join(gopath, "src")
	if err := dirExists(src); err != nil {
		return "", errors.WithMessage(err, "src directory not found")
	}

	return src, nil
}

func mkdir(d string) error {
	return os.Mkdir(d, 0777)
}

func writeFile(path, content string) error {
	return ioutil.WriteFile(path, []byte(content), 0777)
}

func cp(src, dst string, overwrite ...bool) (funcErr error) {
	fw := false
	if len(overwrite) > 0 {
		fw = overwrite[0]
	}
	exists := true
	if _, err := os.Stat(dst); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		exists = false
	}
	if exists && !fw {
		return nil
	}
	fsrc, err := os.Open(src)
	if err != nil {
		return err
	}
	defer fsrc.Close()
	fdst, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		err := fdst.Close()
		if funcErr != nil {
			return
		}
		funcErr = err
	}()
	if _, err := io.Copy(fdst, fsrc); err != nil {
		return err
	}
	return nil
}

func fileExists(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errNotFile
	}
	return nil
}

func dirExists(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errNotDir
	}
	return nil
}

var (
	errNotDir  = errorf("NOT A DIR")
	errNotFile = errorf("NOT A FILE")
)

//-----------------------------------------------------------------------------

type sentinelErr string

func (v sentinelErr) Error() string                { return string(v) }
func errorf(format string, a ...interface{}) error { return sentinelErr(fmt.Sprintf(format, a...)) }

//-----------------------------------------------------------------------------
