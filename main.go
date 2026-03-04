package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	_ "modernc.org/sqlite"
)

const bytesPerLine = 16

type Editor struct {
	app      *tview.Application
	pages    *tview.Pages
	table    *tview.Table
	status   *tview.TextView
	hexData  []byte
	filePath string
	db       *sql.DB
	cursor   int
}

func main() {
	db, err := initDB("ghexeditor.db")
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro ao abrir banco sqlite: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	editor := &Editor{
		app:    tview.NewApplication(),
		pages:  tview.NewPages(),
		table:  tview.NewTable().SetSelectable(true, true),
		status: tview.NewTextView(),
		db:     db,
	}
	editor.status.SetDynamicColors(true)
	editor.status.SetTextAlign(tview.AlignLeft)

	editor.setupLayout()

	if len(os.Args) > 1 {
		if err := editor.loadFile(os.Args[1]); err != nil {
			editor.showError(fmt.Sprintf("falha ao abrir arquivo: %v", err))
		} else {
			editor.setStatus("Arquivo carregado por parâmetro: " + editor.filePath)
		}
	} else {
		editor.showFilePicker()
	}

	if err := editor.app.SetRoot(editor.pages, true).EnableMouse(true).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "erro na interface: %v\n", err)
		os.Exit(1)
	}
}

func initDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	schema := `
CREATE TABLE IF NOT EXISTS sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    file_path TEXT NOT NULL,
    opened_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS edits (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id INTEGER NOT NULL,
    offset INTEGER NOT NULL,
    old_value INTEGER NOT NULL,
    new_value INTEGER NOT NULL,
    edited_at TEXT NOT NULL,
    FOREIGN KEY(session_id) REFERENCES sessions(id)
);
`
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	return db, nil
}

func (e *Editor) setupLayout() {
	e.table.SetBorders(false)
	e.table.SetFixed(1, 1)
	e.table.SetSelectedStyle(tcell.StyleDefault.Background(tcell.ColorBlue).Foreground(tcell.ColorWhite))
	e.table.SetInputCapture(e.handleTableInput)
	e.renderHeader()

	mainFlex := tview.NewFlex().SetDirection(tview.FlexRow)
	mainFlex.AddItem(e.table, 0, 1, true)
	mainFlex.AddItem(e.status, 1, 0, false)

	e.pages.AddPage("main", mainFlex, true, true)
	e.setStatus("Ctrl+O: escolher arquivo | Enter: editar byte | Ctrl+S: salvar | Esc: sair")
}

func (e *Editor) renderHeader() {
	e.table.Clear()
	e.table.SetCell(0, 0, tview.NewTableCell("OFFSET").SetSelectable(false).SetAttributes(tcell.AttrBold))
	for i := 0; i < bytesPerLine; i++ {
		e.table.SetCell(0, i+1, tview.NewTableCell(fmt.Sprintf("%02X", i)).SetSelectable(false).SetAttributes(tcell.AttrBold))
	}
	e.table.SetCell(0, bytesPerLine+1, tview.NewTableCell("ASCII").SetSelectable(false).SetAttributes(tcell.AttrBold))
}

func (e *Editor) renderData() {
	e.renderHeader()
	if len(e.hexData) == 0 {
		return
	}

	rows := (len(e.hexData) + bytesPerLine - 1) / bytesPerLine
	for row := 0; row < rows; row++ {
		offset := row * bytesPerLine
		e.table.SetCell(row+1, 0, tview.NewTableCell(fmt.Sprintf("%06X", offset)).SetSelectable(false))

		ascii := make([]rune, 0, bytesPerLine)
		for col := 0; col < bytesPerLine; col++ {
			idx := offset + col
			cell := tview.NewTableCell("  ")
			if idx < len(e.hexData) {
				b := e.hexData[idx]
				cell = tview.NewTableCell(fmt.Sprintf("%02X", b))
				if b >= 32 && b <= 126 {
					ascii = append(ascii, rune(b))
				} else {
					ascii = append(ascii, '.')
				}
			} else {
				ascii = append(ascii, ' ')
			}
			cell.SetAlign(tview.AlignCenter)
			e.table.SetCell(row+1, col+1, cell)
		}

		e.table.SetCell(row+1, bytesPerLine+1, tview.NewTableCell(string(ascii)).SetSelectable(false))
	}
	e.moveCursorToByte(e.cursor)
}

func (e *Editor) moveCursorToByte(index int) {
	if len(e.hexData) == 0 {
		e.cursor = 0
		e.table.Select(1, 1)
		return
	}
	if index < 0 {
		index = 0
	}
	if index >= len(e.hexData) {
		index = len(e.hexData) - 1
	}
	e.cursor = index
	row := index/bytesPerLine + 1
	col := index%bytesPerLine + 1
	e.table.Select(row, col)
}

func (e *Editor) handleTableInput(event *tcell.EventKey) *tcell.EventKey {
	switch {
	case event.Key() == tcell.KeyCtrlO:
		e.showFilePicker()
		return nil
	case event.Key() == tcell.KeyCtrlS:
		e.saveFile()
		return nil
	case event.Key() == tcell.KeyEsc:
		e.app.Stop()
		return nil
	case event.Key() == tcell.KeyEnter:
		e.showEditModal()
		return nil
	}

	row, col := e.table.GetSelection()
	if row > 0 && col > 0 && col <= bytesPerLine {
		e.cursor = (row-1)*bytesPerLine + (col - 1)
	}
	return event
}

func (e *Editor) showFilePicker() {
	files, err := listFiles(".")
	if err != nil {
		e.showError(err.Error())
		return
	}
	if len(files) == 0 {
		e.showError("nenhum arquivo disponível no diretório atual")
		return
	}

	list := tview.NewList().ShowSecondaryText(false)
	list.SetBorder(true).SetTitle(" Escolha um arquivo ")
	for _, file := range files {
		fileName := file
		list.AddItem(fileName, "", 0, func() {
			if err := e.loadFile(fileName); err != nil {
				e.showError(err.Error())
				return
			}
			e.pages.HidePage("picker")
			e.app.SetFocus(e.table)
		})
	}
	list.AddItem("Cancelar", "", 0, func() {
		e.pages.HidePage("picker")
		e.app.SetFocus(e.table)
	})
	list.SetDoneFunc(func() {
		e.pages.HidePage("picker")
		e.app.SetFocus(e.table)
	})

	modal := centered(70, 20, list)
	e.pages.AddPage("picker", modal, true, true)
	e.app.SetFocus(list)
}

func (e *Editor) loadFile(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	e.filePath = path
	e.hexData = content
	e.cursor = 0
	e.renderData()
	e.logSession(path)
	e.setStatus("Arquivo aberto: " + path)
	e.app.SetFocus(e.table)
	return nil
}

func (e *Editor) showEditModal() {
	if len(e.hexData) == 0 {
		e.showError("abra um arquivo antes de editar")
		return
	}

	idx := e.cursor
	if idx < 0 || idx >= len(e.hexData) {
		return
	}
	current := e.hexData[idx]
	input := tview.NewInputField().
		SetLabel(fmt.Sprintf("Offset %06X valor atual %02X novo valor HEX: ", idx, current)).
		SetText(fmt.Sprintf("%02X", current)).
		SetFieldWidth(4)

	form := tview.NewForm().
		AddFormItem(input).
		AddButton("Salvar", func() {
			value := strings.TrimSpace(input.GetText())
			n, err := strconv.ParseUint(value, 16, 8)
			if err != nil {
				e.showError("valor hexadecimal inválido")
				return
			}
			old := e.hexData[idx]
			e.hexData[idx] = byte(n)
			e.renderData()
			e.moveCursorToByte(idx)
			e.pages.HidePage("edit")
			e.app.SetFocus(e.table)
			e.setStatus(fmt.Sprintf("Byte %06X alterado: %02X -> %02X", idx, old, byte(n)))
			e.logEdit(idx, old, byte(n))
		}).
		AddButton("Cancelar", func() {
			e.pages.HidePage("edit")
			e.app.SetFocus(e.table)
		})
	form.SetBorder(true).SetTitle(" Editar Byte ")

	modal := centered(80, 10, form)
	e.pages.AddPage("edit", modal, true, true)
	e.app.SetFocus(input)
}

func (e *Editor) saveFile() {
	if e.filePath == "" {
		e.showError("nenhum arquivo aberto")
		return
	}
	if err := os.WriteFile(e.filePath, e.hexData, 0o644); err != nil {
		e.showError(fmt.Sprintf("erro ao salvar: %v", err))
		return
	}
	e.setStatus("Arquivo salvo: " + e.filePath)
}

func (e *Editor) showError(msg string) {
	modal := tview.NewModal().
		SetText("Erro: " + msg).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			e.pages.HidePage("error")
			e.app.SetFocus(e.table)
		})
	e.pages.AddPage("error", modal, true, true)
}

func (e *Editor) setStatus(msg string) {
	e.status.SetText(fmt.Sprintf("[yellow]%s", msg))
}

func (e *Editor) currentSessionID() (int64, error) {
	var id int64
	err := e.db.QueryRow(`SELECT id FROM sessions WHERE file_path = ? ORDER BY id DESC LIMIT 1`, e.filePath).Scan(&id)
	return id, err
}

func (e *Editor) logSession(path string) {
	_, _ = e.db.Exec(`INSERT INTO sessions(file_path, opened_at) VALUES(?, ?)`, path, time.Now().Format(time.RFC3339))
}

func (e *Editor) logEdit(offset int, oldValue, newValue byte) {
	sessionID, err := e.currentSessionID()
	if err != nil {
		return
	}
	_, _ = e.db.Exec(
		`INSERT INTO edits(session_id, offset, old_value, new_value, edited_at) VALUES(?, ?, ?, ?, ?)`,
		sessionID, offset, oldValue, newValue, time.Now().Format(time.RFC3339),
	)
}

func centered(w, h int, p tview.Primitive) tview.Primitive {
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(p, h, 1, true).
			AddItem(nil, 0, 1, false), w, 1, true).
		AddItem(nil, 0, 1, false)
}

func listFiles(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext == ".bin" || ext == ".rom" || ext == ".com" || ext == ".bas" || ext == ".dat" || ext == "" {
			files = append(files, name)
		}
	}
	sort.Strings(files)
	return files, nil
}
