package common

import "github.com/aitra-ai/aitra-server/common/types"

type Repo struct {
	Namespace string
	Name      string
	RepoType  types.RepositoryType
	Branch    string
}
