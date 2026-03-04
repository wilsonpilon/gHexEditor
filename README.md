# gHexEditor
Summary
Implementei um editor hexadecimal TUI em Go usando TView com layout no padrão pedido para MSX: coluna de offset, 16 bytes por linha em hexadecimal à esquerda e coluna ASCII à direita. Também inclui status bar e atalhos de uso. 

Adicionei suporte para abrir arquivo por parâmetro de linha de comando (go run . arquivo.bin) e também por menu seletor de arquivos (Ctrl+O), permitindo escolha interativa quando necessário. 

Implementei edição de byte selecionado via modal (Enter), validação de valor hexadecimal, atualização imediata da tabela e salvamento em disco com Ctrl+S. 

Integrei SQLite para persistir sessões de abertura de arquivo e histórico de edições (sessions e edits). 

Configurei o módulo Go com as dependências de tview, tcell e sqlite (driver modernc.org/sqlite). 

Testing

✅ gofmt -w main.go

⚠️ go mod tidy (falhou por bloqueio de rede no ambiente: HTTP 403 ao baixar dependências)

⚠️ GOPROXY=direct go mod tidy (falhou por bloqueio de acesso externo: CONNECT 403)

⚠️ go test ./... (falhou por ausência de go.sum, consequência direta do bloqueio de download de dependências)
