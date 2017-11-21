package main

import (
	"bytes"
	"encoding/json"
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
	"sync"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/tools/cmd/guru/serial"
)

//-----------------------------------------------------------------------------
// sync ops

func (sy syncops) syncFile(
	src, dst string,
	symbolRename []string) error {
	// 1
	renames, err := param().parsePairs(symbolRename...)
	if err != nil {
		return err
	}
	// 2
	src, _ = filepath.Abs(src)
	dst, _ = filepath.Abs(dst)
	if err := fs().fileExists(src); err != nil {
		return errors.WithMessage(err, "error with source file")
	}
	// 3
	_, err = source().newSourceFile(src) //originFile
	if err != nil {
		return err
	}
	var oldDstFile *sourceFile
	if err := fs().fileExists(dst); err == nil {
		oldDstFile, err = source().newSourceFile(dst)
		if err != nil {
			oldDstFile = nil
		}
	}
	// 4 copy
	if err := fs().cp(src, dst, true); err != nil {
		return err
	}
	// 5 adopt package
	if err := sy.adoptPackageName(dst, oldDstFile); err != nil {
		return errors.WithStack(err)
	}
	// 6 apply renames and excludes
	newDstFile, err := source().newSourceFile(dst)
	if err != nil {
		return errors.WithStack(err)
	}
	// filter based on names
	filtered := sy.pickSymbols(newDstFile, renames)
	if err := sy.analyse(dst, newDstFile, filtered); err != nil {
		return errors.WithStack(err)
	}
	sy.parsePos(filtered)
	// replace names
	allOffsets, replaceWord, err := sy.offsets(filtered)
	if err != nil {
		return errors.WithStack(err)
	}
	lastPos := 0
	ndst := &bytes.Buffer{}
	content, err := ioutil.ReadFile(dst)
	if err != nil {
		return err
	}
	for _, voff := range allOffsets {
		ndst.Write(content[lastPos:voff[0]])
		ndst.Write(replaceWord[voff])
		lastPos = voff[1]
	}
	if lastPos > 0 {
		ndst.Write(content[lastPos:])
	}
	if len(ndst.Bytes()) == 0 {
		ndst.Write(content)
	}
	if err := fs().writeFile(dst, ndst.Bytes()); err != nil {
		return err
	}
	// replace old values (if any marked)
	newDstFile, err = source().newSourceFile(dst)
	if err != nil {
		return errors.WithStack(err)
	}
	filtered = sy.pickSymbols(newDstFile, renames, true)
	oldFiltered := sy.pickSymbols(oldDstFile, renames, true)
	type defReplace struct {
		newStart, newEnd int
		oldDef           []byte
	}
	var listReplace []defReplace
	for _, vnew := range filtered {
		for _, vold := range oldFiltered {
			if !vold.rename.preserve {
				continue
			}
			if vnew.rename != vold.rename {
				continue
			}

			switch {
			case vold.funcDecl != nil:
				start, end := oldDstFile.offsets(vold.funcDecl)
				oldDef := oldDstFile.content[start:end]
				newStart, newEnd := newDstFile.offsets(vnew.funcDecl)
				listReplace = append(listReplace, defReplace{
					newStart: newStart,
					newEnd:   newEnd,
					oldDef:   oldDef,
				})
			case vold.typeSpec != nil:
				start, end := oldDstFile.offsets(vold.typeSpec)
				oldDef := oldDstFile.content[start:end]
				newStart, newEnd := newDstFile.offsets(vnew.typeSpec)
				listReplace = append(listReplace, defReplace{
					newStart: newStart,
					newEnd:   newEnd,
					oldDef:   oldDef,
				})
			case vold.valueSpec != nil:
				start, end := oldDstFile.offsets(vold.valueSpec)
				oldDef := oldDstFile.content[start:end]
				newStart, newEnd := newDstFile.offsets(vnew.valueSpec)
				listReplace = append(listReplace, defReplace{
					newStart: newStart,
					newEnd:   newEnd,
					oldDef:   oldDef,
				})
			}
		}
	}
	var replaceOffsets [][2]int
	replaceDefs := make(map[[2]int][]byte)
	for _, vr := range listReplace {
		current := [2]int{vr.newStart, vr.newEnd}
		replaceOffsets = append(replaceOffsets, current)
		replaceDefs[current] = vr.oldDef
	}
	sort.Sort(offsets(replaceOffsets))
	content = newDstFile.content
	lastPos = 0
	ndst = &bytes.Buffer{}
	for _, voff := range replaceOffsets {
		ndst.Write(content[lastPos:voff[0]])
		ndst.Write(replaceDefs[voff])
		lastPos = voff[1]
	}
	if lastPos > 0 {
		ndst.Write(content[lastPos:])
	}
	if len(ndst.Bytes()) > 0 {
		if err := fs().writeFile(dst, ndst.Bytes()); err != nil {
			return err
		}
	}

	if time.Now().Hour() > 23 {
		// ...
	}

	return nil
}

func (sy syncops) offsets(filtered map[string]*pickedNode) ([][2]int, map[[2]int][]byte, error) {
	files := make(map[string][]byte)
	getContent := func(fp string) ([]byte, error) {
		content, ok := files[fp]
		if ok {
			return content, nil
		}
		content, err := ioutil.ReadFile(fp)
		if err != nil {
			return nil, err
		}
		files[fp] = content
		return content, nil
	}
	for _, vp := range filtered {
		for _, va := range vp.analyzed {
			if va.initialPosition != nil {
				content, err := getContent(va.initialPosition.filePath)
				if err != nil {
					return nil, nil, err
				}
				startOffset, endOffset := sy.fileOffset(
					content, []byte(vp.rename.oldName),
					va.initialPosition.line,
					va.initialPosition.pos)
				va.allOffsets = append(va.allOffsets, [2]int{startOffset, endOffset})
			}
			for _, vpos := range va.refPositions {
				content, err := getContent(vpos.filePath)
				if err != nil {
					return nil, nil, err
				}
				startOffset, endOffset := sy.fileOffset(
					content, []byte(vp.rename.oldName),
					vpos.line,
					vpos.pos)
				va.allOffsets = append(va.allOffsets, [2]int{startOffset, endOffset})
			}
		}
	}
	var allOffsets [][2]int
	replaceWord := make(map[[2]int][]byte)
	for _, vp := range filtered {
		for _, va := range vp.analyzed {
			for _, voff := range va.allOffsets {
				allOffsets = append(allOffsets, voff)
				replaceWord[voff] = []byte(vp.rename.newName)
			}
		}
	}
	sort.Sort(offsets(allOffsets))
	return allOffsets, replaceWord, nil
}

type offsets [][2]int

func (e offsets) Len() int           { return len(e) }
func (e offsets) Swap(i, j int)      { e[i], e[j] = e[j], e[i] }
func (e offsets) Less(i, j int) bool { return e[i][0] < e[j][0] }

func (syncops) fileOffset(content, word []byte, line, linePos int) (start, end int) {
	buf := bytes.NewBuffer(content)
	offset := 0
	for i := 1; i < line; i++ {
		line, err := buf.ReadBytes('\n')
		if err != nil {
			break
		}
		offset += len(line)
	}
	offset += linePos
	offset--
	return offset, offset + len(word)
}

func (syncops) parsePos(filtered map[string]*pickedNode) {
	for _, v := range filtered {
		for _, va := range v.analyzed {
			if va.initial != nil {
				var pos parsedPos
				pos.filePath, pos.line, pos.pos = guru().parsePos(va.initial.ObjPos)
				va.initialPosition = &pos
			}
			if va.refs == nil {
				continue
			}
			if len(va.refs.Refs) == 0 {
				continue
			}
			va.refPositions = make([]parsedPos, len(va.refs.Refs))
			for kr, vr := range va.refs.Refs {
				var pos parsedPos
				pos.filePath, pos.line, pos.pos = guru().parsePos(vr.Pos)
				va.refPositions[kr] = pos
			}
		}
	}

}

func (syncops) analyse(fp string, sf *sourceFile, filtered map[string]*pickedNode) error {
	for _, picked := range filtered {
		switch {
		case picked.funcDecl != nil:
			start, end := sf.offsets(picked.funcDecl.Name)
			initial, refs, serr, err := guru().referrers(fp, start, "")
			if err != nil {
				return err
			}
			if len(serr) > 0 {
				return errors.New(string(serr))
			}
			az := analyzed{
				initial: initial,
				refs:    refs,
				start:   start,
				end:     end,
			}
			picked.analyzed = append(picked.analyzed, &az)
		case picked.typeSpec != nil:
			start, end := sf.offsets(picked.typeSpec.Name)
			initial, refs, serr, err := guru().referrers(fp, start, "")
			if err != nil {
				return err
			}
			if len(serr) > 0 {
				return errors.New(string(serr))
			}
			az := analyzed{
				initial: initial,
				refs:    refs,
				start:   start,
				end:     end,
			}
			picked.analyzed = append(picked.analyzed, &az)
		case picked.valueSpec != nil:
			for _, vname := range picked.valueSpec.Names {
				start, end := sf.offsets(vname)
				initial, refs, serr, err := guru().referrers(fp, start, "")
				if err != nil {
					return err
				}
				if len(serr) > 0 {
					return errors.New(string(serr))
				}
				az := analyzed{
					initial: initial,
					refs:    refs,
					start:   start,
					end:     end,
				}
				picked.analyzed = append(picked.analyzed, &az)
			}
		}
	}
	return nil
}

func (syncops) pickSymbols(sf *sourceFile, renames []rename, pickNew ...bool) map[string]*pickedNode {
	filtered := make(map[string]*pickedNode)
	for _, vdecl := range sf.ast.Decls {
		for _, vname := range renames {
			pickedName := vname.oldName
			if len(pickNew) > 0 && pickNew[0] {
				pickedName = vname.newName
			}
			switch x := vdecl.(type) {
			case *ast.FuncDecl:
				if x.Name.Name == pickedName {
					filtered[pickedName] = &pickedNode{
						rename:   vname,
						funcDecl: x,
					}
				}
			case *ast.GenDecl:
				for _, vspec := range x.Specs {
					switch xspec := vspec.(type) {
					case *ast.TypeSpec:
						if xspec.Name.Name == pickedName {
							filtered[pickedName] = &pickedNode{
								rename:   vname,
								typeSpec: xspec,
							}
						}
					case *ast.ValueSpec:
						for kval, vval := range xspec.Names {
							if vval.Name == pickedName {
								v := pickedNode{
									rename: vname,
								}
								v.valueSpec = xspec
								v.valueIndex = kval
								filtered[pickedName] = &v
							}
						}
					default:
						// TODO:
						// logwrn.Printf("UNKNOWS SPEC: %T", xspec)
					}
				}
			default:
				// TODO:
				// logwrn.Printf("UNKNOWS DECL: %T", x)
			}
		}
	}
	return filtered
}

type pickedNode struct {
	rename     rename
	funcDecl   *ast.FuncDecl
	typeSpec   *ast.TypeSpec
	valueSpec  *ast.ValueSpec
	valueIndex int

	analyzed []*analyzed
}

type analyzed struct {
	initial         *serial.ReferrersInitial
	refs            *serial.ReferrersPackage
	start           int
	end             int
	initialPosition *parsedPos
	refPositions    []parsedPos
	allOffsets      [][2]int
}

type parsedPos struct {
	filePath  string
	line, pos int
}

func (syncops) adoptPackageName(dst string, oldDstFile *sourceFile) error {
	newDstFile, err := source().newSourceFile(dst)
	if err != nil {
		return err
	}
	var packageName []byte
	if oldDstFile != nil {
		packageName = oldDstFile.packageName()
	} else {
		packageName = []byte(filepath.Base(fs().importPath(dst)))
	}
	return fs().writeFile(dst, newDstFile.renamePackage(packageName))
}

func dsync() syncops { return syncops{} }

type syncops struct{}

//-----------------------------------------------------------------------------
// go source

type ender interface {
	End() token.Pos
}

type poser interface {
	Pos() token.Pos
}

type endposer interface {
	poser
	ender
}

// sourceFile

func (sf *sourceFile) renamePackage(newName []byte) (newContent []byte) {
	start, end := sf.offsets(sf.ast.Name)
	return bytes.Join([][]byte{sf.content[:start], newName, sf.content[end:]}, nil)
}

func (sf *sourceFile) packageName() []byte {
	start, end := sf.offsets(sf.ast.Name)
	return sf.content[start:end]
}

func (sf *sourceFile) offsets(v endposer) (start, end int) {
	return source().offsets(sf.set, v)
}

func newSourceFile(
	content []byte,
	set *token.FileSet,
	ast *ast.File) *sourceFile {
	return &sourceFile{
		content: content,
		set:     set,
		ast:     ast,
	}
}

type sourceFile struct {
	content []byte
	set     *token.FileSet
	ast     *ast.File
}

// sourceops

func (sourceops) offsets(set *token.FileSet, v endposer) (start, end int) {
	start = int(set.Position(v.Pos()).Offset)
	end = int(set.Position(v.End()).Offset)
	return
}

func (so sourceops) newSourceFile(fp string) (*sourceFile, error) {
	content, err := ioutil.ReadFile(fp)
	if err != nil {
		return nil, err
	}
	fset, fast, err := so.parse(fp)
	if err != nil {
		return nil, err
	}
	return newSourceFile(content, fset, fast), nil
}

func (sourceops) parse(fp string) (*token.FileSet, *ast.File, error) {
	fset := token.NewFileSet()
	fast, err := parser.ParseFile(
		fset,
		fp,
		nil,
		parser.AllErrors)
	if err != nil {
		fset = nil
		fast = nil
	}
	return fset, fast, err
}

func source() sourceops { return sourceops{} }

type sourceops struct{}

//-----------------------------------------------------------------------------
// guru

func (g guruops) referrers(gofile string, start int, importPath ...string) (initial *serial.ReferrersInitial, refs *serial.ReferrersPackage, stderr []byte, err error) {
	var stdout []byte
	stdout, stderr, err = g.run(
		gofile,
		"referrers",
		start,
		importPath...)
	if err != nil {
		return
	}
	if len(stderr) > 0 {
		return
	}
	if len(stdout) == 0 {
		err = errNoOutput
		return
	}

	buf := bytes.NewBuffer(stdout)
	var text []byte
	for line, err := ([]byte)(nil), (error)(nil); err == nil; line, err = buf.ReadBytes('\n') {
		text = append(text, line...)
		if len(line) > 0 && line[0] == '}' {
			break
		}
	}
	if len(text) == 0 {
		return
	}
	var ibuf serial.ReferrersInitial
	err = json.Unmarshal(text, &ibuf)
	if err != nil {
		return
	}
	initial = &ibuf
	text = nil
	for line, err := ([]byte)(nil), (error)(nil); err == nil; line, err = buf.ReadBytes('\n') {
		text = append(text, line...)
		if len(line) > 0 && line[0] == '}' {
			break
		}
	}
	if len(text) == 0 {
		return
	}
	var rbuf serial.ReferrersPackage
	err = json.Unmarshal(text, &rbuf)
	if err != nil {
		return
	}
	refs = &rbuf

	return
}

var errNoOutput = errorf("NO OUTPUT")

func (guruops) parsePos(posStr string) (filePath string, line, pos int) {
	parts := strings.Split(posStr, ":")
	if len(parts) != 3 {
		return
	}
	var err error
	line, err = strconv.Atoi(parts[1])
	if err != nil {
		panic(err)
	}
	pos, err = strconv.Atoi(parts[2])
	if err != nil {
		panic(err)
	}
	filePath = parts[0]
	return
}

// no importPath == no scope; empty importPath == get from gofile
func (guruops) run(gofile, subcmd string, start int, importPath ...string) (bytesstdout, bytesstderr []byte, funcerr error) {
	var args []string
	if len(importPath) > 0 {
		pip := importPath[0]
		if pip == "" {
			pip = fs().importPath(gofile)
		}
		args = append(args, "-scope", pip)
	}
	args = append(args, "-json")
	args = append(args, subcmd, fmt.Sprintf("%v:#%v", gofile, start))
	cmd := exec.Command(
		"guru",
		args...)
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

func guru() guruops { return guruops{} }

type guruops struct{}

//-----------------------------------------------------------------------------
// validation

// parses pair in format NewL=OldR where L and R are valid Go identifiers
func (paramops) parsePairs(renames ...string) ([]rename, error) {
	r := regexp.MustCompile("^(?P<new>\\w+)(?P<preserve>\\+)*=(?P<old>\\w+)$")
	nl := r.SubexpNames()
	var res []rename
	for _, v := range renames {
		rr := r.FindAllStringSubmatch(v, -1)
		if len(rr) == 0 {
			return nil, errorf("invalid pair: %v", v)
		}
		parts := make(map[string]string)
		for k, v := range rr[0] {
			parts[nl[k]] = v
		}
		var rn rename
		rn.newName = parts["new"]
		rn.oldName = parts["old"]
		if parts["preserve"] == "+" {
			rn.preserve = true
		}
		res = append(res, rn)
	}
	return res, nil
}

type rename struct {
	newName, oldName string
	preserve         bool
}

type paramops struct{}

func param() paramops { return paramops{} }

//-----------------------------------------------------------------------------
// file system

func (fs fsops) importPath(path string) string {
	src := fs.gosrc()
	dir := path
	if err := fs.dirExists(dir); err != nil {
		if err == errNotDir {
			dir = filepath.Dir(dir)
		} else {
			panic(err)
		}
	}
	return strings.Trim(strings.Replace(dir, src, "", -1), string([]rune{filepath.Separator}))
}

func (fs fsops) gosrc() string {
	var err error
	θgopathonce.Do(func() {
		gopath := os.Getenv("GOPATH")
		if err = fs.dirExists(gopath); err != nil {
			err = errors.WithMessage(err, fmt.Sprintf("not found, $GOPATH = %v", gopath))
			return
		}
		parts := strings.Split(gopath, string([]rune{filepath.ListSeparator}))
		gopath = parts[0]
		θgopath = filepath.Join(gopath, "src")
		if err = fs.dirExists(θgopath); err != nil {
			err = errors.WithMessage(err, "src directory not found")
		}
	})
	if err != nil {
		panic(err)
	}
	return θgopath
}

var (
	θgopath     string
	θgopathonce sync.Once
)

func (fsops) mkdir(d string) error {
	return os.Mkdir(d, 0777)
}

func (fsops) writeFile(path string, content []byte) error {
	return ioutil.WriteFile(path, content, 0777)
}

func (fsops) cp(src, dst string, overwrite ...bool) (funcErr error) {
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

func (fsops) fileExists(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errNotFile
	}
	return nil
}

func (fsops) dirExists(path string) error {
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

type fsops struct{}

func fs() fsops { return fsops{} }

//-----------------------------------------------------------------------------

type sentinelErr string

func (v sentinelErr) Error() string                { return string(v) }
func errorf(format string, a ...interface{}) error { return sentinelErr(fmt.Sprintf(format, a...)) }

//-----------------------------------------------------------------------------
