# dockerinspec

TUI (Go + Bubble Tea) para visualizar containers Docker.

- **Canvas** (`1`): containers agrupados por projeto docker-compose, ligados por setas quando se comunicam.
  As relações vêm das variáveis de ambiente (`localhost:PORT`, `host.docker.internal:PORT` → container que publica a porta;
  `http://servico:PORT`, `DB_HOST=servico` → container resolvível numa rede compartilhada). Linhas tracejadas = via host.
  Navegue com setas/hjkl (Shift = mais rápido), roda do mouse (Shift+roda = horizontal) ou arrastando o fundo.
  Clique num container para ver os logs (painel inferior; `esc`/✕ fecha). `e` mostra as portas nas setas.
- **Zoom** (canvas): `+`/`-` (ou Ctrl+roda, ou os botões − + no cabeçalho), `0` volta a 100%. Quatro níveis: 25% e 50% compactam os cartões, 150% mostra imagem, status, portas e redes.
- **Tamanho dos logs**: `[`/`]` diminuem/aumentam, ou arraste a borda (título do painel no canvas, divisória vertical na lista).
- **Copiar logs**: arraste sobre o texto dos logs (seleciona e copia ao soltar); duplo clique copia a linha inteira; `y` copia de novo a seleção, `Y` copia todo o buffer, `esc` limpa a seleção. A cópia vai para o buffer do tmux, `wl-copy`/`xclip`/`pbcopy` (o que existir) e OSC 52 (funciona via SSH). A seleção usa o texto original, sem as quebras de linha da tela.
- **Lista** (`2`): containers à esquerda, logs à direita.
- **Ações no container selecionado**: `s` inicia/para, `R` reinicia, `d` (ou `Del`) deleta com `docker rm -f` após confirmação (`y`/`Enter`).
- `tab` alterna as vistas, `n`/`p` próximo/anterior, `PgUp/PgDn` rolam logs, `End` volta a seguir, `r` atualiza, `q` sai.

Dentro do tmux o mouse exige `set -g mouse on` (o app avisa no rodapé se estiver desligado).

    go build -o dockerinspec . && ./dockerinspec      # ./dockerinspec -dump imprime o canvas em texto
