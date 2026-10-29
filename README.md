[38;2;131;148;150m─────┬──────────────────────────────────────────────────────────────────────────[0m
     [38;2;131;148;150m│ [0m[1mSTDIN[0m
[38;2;131;148;150m─────┼──────────────────────────────────────────────────────────────────────────[0m
[38;2;131;148;150m   1[0m [38;2;131;148;150m│[0m [38;2;248;248;242m# dockerinspec[0m
[38;2;131;148;150m   2[0m [38;2;131;148;150m│[0m 
[38;2;131;148;150m   3[0m [38;2;131;148;150m│[0m [38;2;248;248;242mTUI (Go + Bubble Tea) para visualizar containers Docker.[0m
[38;2;131;148;150m   4[0m [38;2;131;148;150m│[0m 
[38;2;131;148;150m   5[0m [38;2;131;148;150m│[0m [38;2;248;248;242m- **Canvas** (`1`): containers agrupados por projeto docker-compose, ligados por setas quando se comunicam.[0m
[38;2;131;148;150m   6[0m [38;2;131;148;150m│[0m [38;2;248;248;242m  As relações vêm das variáveis de ambiente (`localhost:PORT`, `host.docker.internal:PORT` → container que publica a porta;[0m
[38;2;131;148;150m   7[0m [38;2;131;148;150m│[0m [38;2;248;248;242m  `http://servico:PORT`, `DB_HOST=servico` → container resolvível numa rede compartilhada). Linhas tracejadas = via host.[0m
[38;2;131;148;150m   8[0m [38;2;131;148;150m│[0m [38;2;248;248;242m  Navegue com setas/hjkl (Shift = mais rápido), roda do mouse (Shift+roda = horizontal) ou arrastando o fundo.[0m
[38;2;131;148;150m   9[0m [38;2;131;148;150m│[0m [38;2;248;248;242m  Clique num container para ver os logs (painel inferior; `esc`/✕ fecha). `e` mostra as portas nas setas.[0m
[38;2;131;148;150m  10[0m [38;2;131;148;150m│[0m [38;2;248;248;242m- **Zoom** (canvas): `+`/`-` (ou Ctrl+roda, ou os botões − + no cabeçalho), `0` volta a 100%. Quatro níveis: 25% e 50% compactam os cartões, 150% mostra imagem, status, portas e redes.
- **Tamanho dos logs**: `[`/`]` diminuem/aumentam, ou arraste a borda (título do painel no canvas, divisória vertical na lista).
- **Copiar logs**: arraste sobre o texto dos logs (seleciona e copia ao soltar); duplo clique copia a linha inteira; `y` copia de novo a seleção, `Y` copia todo o buffer, `esc` limpa a seleção. A cópia vai para o buffer do tmux, `wl-copy`/`xclip`/`pbcopy` (o que existir) e OSC 52 (funciona via SSH). A seleção usa o texto original, sem as quebras de linha da tela.
- **Lista** (`2`): containers à esquerda, logs à direita.[0m
[38;2;131;148;150m  11[0m [38;2;131;148;150m│[0m [38;2;248;248;242m- `tab` alterna as vistas, `n`/`p` próximo/anterior, `PgUp/PgDn` rolam logs, `End` volta a seguir, `r` atualiza, `q` sai.[0m
[38;2;131;148;150m  12[0m [38;2;131;148;150m│[0m 
[38;2;131;148;150m  13[0m [38;2;131;148;150m│[0m [38;2;248;248;242mDentro do tmux o mouse exige `set -g mouse on` (o app avisa no rodapé se estiver desligado).[0m
[38;2;131;148;150m  14[0m [38;2;131;148;150m│[0m 
[38;2;131;148;150m  15[0m [38;2;131;148;150m│[0m [38;2;248;248;242m    go build -o dockerinspec . && ./dockerinspec      # ./dockerinspec -dump imprime o canvas em texto[0m
[38;2;131;148;150m─────┴──────────────────────────────────────────────────────────────────────────[0m
