package app

import (
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// specCompare matches workspace values, including unexported fields and errors.
var specCompare = cmp.Options{
	cmp.AllowUnexported(WorkspaceSpec{}, ProjectSpec{}),
	cmpopts.EquateErrors(),
}
