# faustlsp

A LSP Server for the Faust programming language.

# Installation

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

For code formatting, install [faustfmt](https://github.com/carn181/faustfmt) following install instructions in the project's README.

# Usage

## VS Code

[vscode-faust](https://github.com/carn181/vscode-faust) is a VS Code extension for Faust that works with faustlsp. Follow installation steps in the README.md

## Neovim

Sample nvim-lspconfig configuration which requires a .faustcfg.json in the faust project root directory:
```lua
vim.lsp.config('faustlsp', {
    cmd = { 'faustlsp' },
    filetypes = {'faust'},
	workspace_required = true,
	root_markers = { '.faustcfg.json' }
})
```

## Emacs

Sample lsp-mode server config
```lisp
(lsp-register-client
   (make-lsp-client
    :new-connection (lsp-stdio-connection "faustlsp")
    :activation-fn (lsp-activate-on "faust")
    :server-id 'faustlsp
    ))
```


# Features

- [x] Document Synchronization
- [x] Diagnostics
  - [x] Syntax Errors
  - [x] Compiler Errors (can disable in .faustcfg.json as they look ugly due to compiler limitations)
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

## 📜 License

This project is released under the terms of the **GNU General Public License, Version 3 (GPLv3) or any later version**.

**Copyright (C) 2025 Ryan Biju Varghese**

You can find the full text of the license in the **`LICENSE`** file at the root of this repository.
