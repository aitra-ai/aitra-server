package migrations

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

type aiSkill struct {
	bun.BaseModel  `bun:"table:ai_skills"`
	ID             int64     `bun:",pk,autoincrement"`
	Name           string    `bun:",notnull"`
	Description    string    `bun:",notnull,default:''"`
	SystemPrompt   string    `bun:",notnull"`
	PreferredModel string    `bun:",notnull,default:''"`
	Icon           string    `bun:",notnull,default:'🤖'"`
	IsBuiltin      bool      `bun:",notnull,default:false"`
	Enabled        bool      `bun:",notnull,default:true"`
	CreatedAt      time.Time `bun:",notnull,default:current_timestamp"`
	UpdatedAt      time.Time `bun:",notnull,default:current_timestamp"`
}

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		_, err := db.NewCreateTable().Model((*aiSkill)(nil)).IfNotExists().Exec(ctx)
		if err != nil {
			return err
		}

		// Seed builtin skills
		now := time.Now()
		builtins := []aiSkill{
			{
				Name:           "Code Assistant",
				Description:    "Expert coding assistant for writing, reviewing, and debugging code",
				SystemPrompt:   "You are an expert software engineer. Help the user write, review, debug, and optimize code. Always provide clean, well-commented code with explanations. When fixing bugs, explain the root cause. Support all major programming languages including Python, JavaScript, TypeScript, Go, Java, C++, and Rust.",
				PreferredModel: "claude-sonnet-4-20250514",
				Icon:           "💻",
				IsBuiltin:      true,
				Enabled:        true,
				CreatedAt:      now,
				UpdatedAt:      now,
			},
			{
				Name:           "Translator",
				Description:    "Professional multilingual translator with natural, fluent output",
				SystemPrompt:   "You are a professional translator. Translate text between any languages while preserving meaning, tone, and context. When the target language is not specified, translate to English. For ambiguous phrases, provide alternative translations. Keep formatting (markdown, code blocks) intact. Do not add explanations unless asked.",
				PreferredModel: "gpt-4o",
				Icon:           "🌐",
				IsBuiltin:      true,
				Enabled:        true,
				CreatedAt:      now,
				UpdatedAt:      now,
			},
			{
				Name:           "Document Summary",
				Description:    "Summarize long documents, articles, and reports into concise overviews",
				SystemPrompt:   "You are a document summarization expert. When given text, produce a clear, structured summary that captures the key points, main arguments, and conclusions. Use bullet points for clarity. For long documents, provide both a TL;DR (1-2 sentences) and a detailed summary. Preserve important data, numbers, and quotes.",
				PreferredModel: "deepseek-chat",
				Icon:           "📝",
				IsBuiltin:      true,
				Enabled:        true,
				CreatedAt:      now,
				UpdatedAt:      now,
			},
		}

		for _, s := range builtins {
			// Only insert if not exists (by name)
			exists, _ := db.NewSelect().Model((*aiSkill)(nil)).Where("name = ?", s.Name).Exists(ctx)
			if !exists {
				_, err := db.NewInsert().Model(&s).Exec(ctx)
				if err != nil {
					return err
				}
			}
		}
		return nil
	}, func(ctx context.Context, db *bun.DB) error {
		_, err := db.NewDropTable().Model((*aiSkill)(nil)).IfExists().Exec(ctx)
		return err
	})
}
