# goreuse
A tool for reuse Go code, that bundles a whole package inside a single file. It allows to rename certain identifiers and keep the changed definitions. Also it supports `go generate` by adding the necessary comments.

# installation

Go get this tool by:

```bash
$ go get -u -v github.com/dc0d/goreuse
```

# usage

- To just bundle another package and write the code to a file:

```bash
goreuse -o some-file.go the/package/to/bundle
```

- For adding a certain prefix to all identifiers inside the bundled code (default value is the package name):

```bash
goreuse -o some-file.go -prefix prefx_ the/package/to/bundle
```

- To rename some individual identifiers (`-rn` flag can be repeated):

```bash
goreuse -rn newName=oldName -o some-file.go -prefix prefx_ the/package/to/bundle
```


- To rename some individual identifiers _and_ preserve the changes that are made to it's definition (note the `+` sign in `newName+=oldName`):

```bash
goreuse -rn newName+=oldName -o some-file.go -prefix prefx_ the/package/to/bundle
```
