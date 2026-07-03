package workflows

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/aitra-ai/aitra-server/common/types"
	dvCom "github.com/aitra-ai/aitra-server/dataviewer/common"
)

func TestRepoFiles_appendFile(t *testing.T) {
	file := &types.File{
		Name: "test.jsonl",
		Size: 101,
	}

	fileClass := dvCom.RepoFilesClass{
		AllFiles:     make(map[string]*dvCom.RepoFile),
		ParquetFiles: make(map[string]*dvCom.RepoFile),
		JsonlFiles:   make(map[string]*dvCom.RepoFile),
		CsvFiles:     make(map[string]*dvCom.RepoFile),
	}

	appendFile(file, &fileClass, 100)

	require.Equal(t, 1, len(fileClass.AllFiles))
	require.Equal(t, 1, len(fileClass.JsonlFiles))
}
