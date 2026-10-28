package loader

import (
	"go/ast"
	"go/token"
	"go/types"
)

// reflectPackages declare reflect.Value.Method and MethodByName.
var reflectPackages = map[string]bool{"reflect": true, "internal/reflectlite": true}

// UsesReflectMethod reports whether the program refers to reflect.Value.Method
// or MethodByName, directly or through a function of reflect that does.
func (p *Program) UsesReflectMethod() bool {
	targets := make(map[types.Object]bool)
	for path := range reflectPackages {
		pkg := p.Packages[path]
		if pkg == nil || pkg.Pkg == nil {
			continue
		}
		obj := pkg.Pkg.Scope().Lookup("Value")
		if obj == nil {
			continue
		}
		named, ok := obj.Type().(*types.Named)
		if !ok {
			continue
		}
		for i := 0; i < named.NumMethods(); i++ {
			if m := named.Method(i); m.Name() == "Method" || m.Name() == "MethodByName" {
				targets[m] = true
			}
		}
	}
	if len(targets) == 0 {
		return false
	}

	// A function of reflect that refers to a target is a target itself.
	for changed := true; changed; {
		changed = false
		for path := range reflectPackages {
			pkg := p.Packages[path]
			if pkg == nil {
				continue
			}
			for ident, obj := range pkg.info.Uses {
				if !targets[obj] {
					continue
				}
				if fn := pkg.enclosingFunc(ident.Pos()); fn != nil && !targets[fn] {
					targets[fn] = true
					changed = true
				}
			}
		}
	}

	for _, pkg := range p.Packages {
		for ident, obj := range pkg.info.Uses {
			if !targets[obj] {
				continue
			}
			if reflectPackages[pkg.ImportPath] {
				if fn := pkg.enclosingFunc(ident.Pos()); fn != nil && targets[fn] {
					continue
				}
			}
			return true
		}
	}
	return false
}

// enclosingFunc returns the function or method declared around pos.
func (p *Package) enclosingFunc(pos token.Pos) types.Object {
	for _, file := range p.Files {
		if pos < file.Pos() || pos >= file.End() {
			continue
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Pos() <= pos && pos < fn.End() {
				return p.info.Defs[fn.Name]
			}
		}
	}
	return nil
}
