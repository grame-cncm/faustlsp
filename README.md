# faustlsp

A LSP Server for the Faust programming language.

# Usage

To install, run  
```sh
    go get github.com/carn181/faustlsp@latest
```

This will install a `faustlsp` executable in `$HOME/go/bin` by default.  


Alternatively, you can clone this repository, build and install faustlsp.  

```sh
git clone https://github.com/carn181/faustlsp
cd faustlsp
go build
go install
```

# Features

- [x] Document Synchronization
- [x] Diagnostics
  - [x] Syntax Errors
  - [x] Compiler Errors (can disable as they look ugly due to compiler limitations)
- [x] Hover Documentation
- [x] Code Completion
- [x] Document Symbols
- [x] Formatting (using [faustfmt](https://github.com/carn181/faustfmt))
- [x] Goto Definition
- [ ] Find References

# Configuration

You can configure the LSP server and it give it information about the project using a `.faustcfg.json` file defined in a project's root directory.  
Configuration Options:  
```js
{
  "command": "faust",              // Faust Compiler Executable to use
  "process_name": "process",       // Process Name passed as -pn to compiler
  "process_files": ["a.dsp"],      // Files that have top-level processes defined
  "compiler_diagnostics": true     // Show Compiler Errors 
}
```

# VS Code

[vscode-faust](https://github.com/carn181/vscode-faust) is a VS Code extension for Faust that works with faustlsp.
