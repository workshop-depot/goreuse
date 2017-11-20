# goreuse
A tool for reuse Go code, that keeps two files in sync, taking type definitions into consideration. It can be used with `go generate` too.

# installation

This tool uses Go Guru tool. Go get it by:

```bash
$ go get -u -v golang.org/x/tools/cmd/guru
```

Then go get this tool by:

```bash
$ go get -u -v github.com/dc0d/goreuse
```

# sample usage (TODO)

This command syncs these two files, then customizes the type names and if the types are already redefined, it would preserve the new definition.

```
$ goreuse file --src ./code-templates/generic-map.go --dst ./name-count-map.go -t nameKey=tkey -t countVal=tval -t nameCount=safemap
```

# tests (TODO)