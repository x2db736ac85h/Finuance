package httpapi

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"maps"
	"mime"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/httpapi/gen"
	"github.com/x2db736ac85h/finuance/backend/internal/ledger"
	"github.com/x2db736ac85h/finuance/backend/internal/notify"
	"github.com/x2db736ac85h/finuance/backend/internal/observability"
	"github.com/x2db736ac85h/finuance/backend/internal/query"
)

// ExportFormatVersion is the version of the export layout written to manifest.json.
const ExportFormatVersion = 1

// ExportMe streams a zip of everything the user owns (PLAN task 7.10, §8.1 privacy):
//
//	manifest.json           format version, export time, the user
//	<entity>.json           accounts, categories, groups, budgets, annotations, dedupe overrides,
//	                        imports (metadata), investments, preferences, assistant (settings and
//	                        chats with their turns), rules, category aliases, activity (the
//	                        audit log), friends (display names only), notifications
//	transactions.csv        every transaction of every account (the CSV export format, all columns)
//	imports/<id>/<filename> the original files
//
// The JSON documents are read before the response starts, so a failure there is still a problem
// response; the CSV and the files then stream.
func (a *api) ExportMe(ctx context.Context, _ gen.ExportMeRequestObject) (gen.ExportMeResponseObject, error) {
	id := identity(ctx)
	u := id.User.ID
	docs, err := a.ledger.Export(ctx, u)
	if err != nil {
		return nil, err
	}
	prefs, err := a.auth.GetPreferences(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.ai != nil {
		doc, err := a.assistantExport(ctx, u)
		if err != nil {
			return nil, err
		}
		docs["assistant.json"] = doc
	}
	if a.friends != nil {
		list, err := a.friends.List(ctx, u)
		if err != nil {
			return nil, err
		}
		out := make([]gen.Friend, len(list))
		for i, f := range list {
			out[i] = friendDTO(f)
		}
		docs["friends.json"] = out
	}
	if a.notify != nil {
		list, _, err := a.notify.List(ctx, u, notify.MaxList)
		if err != nil {
			return nil, err
		}
		out := make([]gen.Notification, len(list))
		for i, n := range list {
			out[i] = gen.Notification{Id: n.ID, Kind: gen.NotificationKind(n.Kind), Data: n.Data, CreatedAt: n.CreatedAt, ReadAt: n.ReadAt}
		}
		docs["notifications.json"] = out
	}
	a.audit(ctx, u, ledger.AuditExportData, "", nil)
	now := time.Now().UTC()
	docs["preferences.json"] = map[string]any{"version": prefs.Version, "prefs": prefs.Prefs}
	docs["manifest.json"] = map[string]any{"format": "finuance-export", "version": ExportFormatVersion, "exportedAt": now,
		"user": map[string]any{"id": u, "email": id.User.Email, "displayName": id.User.DisplayName, "createdAt": id.User.CreatedAt}}

	pr, pw := io.Pipe()
	go func() {
		err := a.writeExportZip(ctx, pw, u, docs, now)
		if err != nil {
			observability.Logger(ctx).WarnContext(ctx, "data export stopped", "error", err)
		}
		_ = pw.CloseWithError(err)
	}()
	cd := mime.FormatMediaType("attachment", map[string]string{"filename": "finuance-export-" + now.Format("2006-01-02") + ".zip"})
	return gen.ExportMe200ApplicationzipResponse{Body: pr, Headers: gen.ExportMe200ResponseHeaders{ContentDisposition: &cd}}, nil
}

// assistantExport is the assistant's settings and every chat as the panel shows it.
func (a *api) assistantExport(ctx context.Context, u uuid.UUID) (map[string]any, error) {
	s, err := a.ai.Chats.Settings(ctx, u)
	if err != nil {
		return nil, err
	}
	list, err := a.ai.Chats.List(ctx, u)
	if err != nil {
		return nil, err
	}
	chats := make([]gen.AssistantChatDetail, 0, len(list))
	for _, c := range list {
		ch, msgs, err := a.ai.Chats.Get(ctx, u, c.ID)
		if err != nil {
			return nil, err
		}
		chats = append(chats, gen.AssistantChatDetail{Id: ch.ID, Title: ch.Title, Connection: ch.Connection, Model: ch.Model,
			CreatedAt: ch.CreatedAt, UpdatedAt: ch.UpdatedAt, NoTools: ch.NoTools, Turns: turnsOf(msgs)})
	}
	return map[string]any{"settings": settingsDTO(s), "chats": chats}, nil
}

func (a *api) writeExportZip(ctx context.Context, w io.Writer, userID uuid.UUID, docs map[string]any, now time.Time) error {
	z := zip.NewWriter(w)
	create := func(name string) (io.Writer, error) {
		return z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: now})
	}
	names := slices.Sorted(maps.Keys(docs))
	for _, name := range names {
		f, err := create(name)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		if err := enc.Encode(docs[name]); err != nil {
			return err
		}
	}

	f, err := create("transactions.csv")
	if err != nil {
		return err
	}
	cols := query.GridOrder
	header := make([]string, len(cols))
	for i, c := range cols {
		col, _ := query.Lookup(c)
		header[i] = col.Label
	}
	if _, err := io.WriteString(f, "\ufeff"+query.CSVLine(header, ";")); err != nil {
		return err
	}
	if err := a.query.Export(ctx, userID, query.Context{IncludeTransfers: true}, nil, cols, func(row []string) error {
		_, err := io.WriteString(f, "\r\n"+query.CSVLine(row, ";"))
		return err
	}); err != nil {
		return err
	}

	if err := a.ledger.ExportFiles(ctx, userID, func(imp ledger.Import, content []byte) error {
		f, err := create("imports/" + imp.ID.String() + "/" + zipName(imp.Filename))
		if err != nil {
			return err
		}
		_, err = f.Write(content)
		return err
	}); err != nil {
		return err
	}
	return z.Close()
}

// zipName keeps a stored filename safe as a zip entry: no directories, no "..".
func zipName(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	if name == "." || name == ".." || name == "/" || name == "" {
		return "file.csv"
	}
	return name
}
