# finyap
Learn Finnish by writing Finnish. With guardrails, of course.

![output](https://github.com/user-attachments/assets/d7fb47b3-ad88-41b9-9b40-da1bd4dfa725)

## How it looks

The above GIF is actually from v0.0.1 of the game (it feels like a game to us, at least). We keep it here because it's so stripped down there's almost nothing to do *but* focus on the core gameplay loop. Here are some shots of it slowed down.

![output-slow](https://github.com/user-attachments/assets/56d67e19-3b72-4bd0-ba6b-d50dd3d84e10)

![output-slower](https://github.com/user-attachments/assets/b00170ab-2675-453a-8b48-48f4081389e3)

v0.0.2 added both **timing**, so you can see which words were harder and which words were eaiser for you to guess, as well as **scenarios** and a `practice-scenarios.bash` script, which runs through a all of the scenarios a handful of times. This GIF shows a brief play session using the practice scenarios script.

![output-finyap](https://github.com/user-attachments/assets/787eb9e5-194a-450e-8e6c-001320e7c50f)


## Requirements

**This project is still in early development.** To run `finyap.bash`, you *must* have the following programs installed:

- **A UTF-8 compatible terminal**: Required to correctly display Finnish characters (like `ä` and `ö`) and ANSI color codes. I use [Kitty](https://sw.kovidgoyal.net/kitty/); [Alacritty](https://alacritty.org/) should work too if you're having trouble.
- Bash, the Bourne-again shell. If you're on Mac or Linux, you probably already have this. If you're on Windows, maybe try [Windows Subsystem for Linux](https://www.howtogeek.com/790062/how-to-install-bash-on-windows-11/)?
- **[fzf](https://github.com/junegunn/fzf)**, the fuzzy command line finder. **Essential**, this script will not run without `fzf` installed!
  - **Homebrew (macOS):**
    ```bash
    brew install fzf
    ```
  - **APT (Debian/Ubuntu):**
    ```bash
    sudo apt-get install fzf
    ```
  - **Pacman (Arch Linux):**
    ```bash
    sudo pacman -S fzf
    ```
  - For other systems, follow the [official fzf installation instructions](https://www.google.com/search?q=https://github.com/junegunn/fzf%23installation).
- **Standard Unix utilities**: `shuf`, `sed`, `grep`, `tr`, `cut`, `head`. These are pre-installed on most Linux and macOS systems.

## Installation

1.  **Clone the repository:**

    ```bash
    git clone https://github.com/hiAndrewQuinn/finyap.git
    cd finyap
    ```

2.  Install `fzf`, if you haven't already.

3.  Run the script with `bash finyap.bash`.

## **Command-line Options:**

- Show the help message:
  ```bash
  bash finyap.bash --help
  ```
- Show the script version:
  ```bash
  bash finyap.bash --version
  ```
- Play `finyap` using a custom TSV file, for example the ones included in `scenarios/`:
  ```bash
  bash finyap.bash --input scenarios/ordering-coffee.tsv
  ```

## Configuration

No configuration is provided out of the box. That might change in the future, though, as this project is in very early development.

## How It Works

### Gameplay Loop

1.  **Sampling**: The script reads `SAMPLED_LINES_COUNT` random lines from the `SENTENCE_FILE`.
2.  **Word List Creation**: All unique Finnish words from the sampled lines are collected to create the master list for `fzf`.
3.  **Sentence Selection**: One random sentence is chosen from the sample for the game round.
4.  **Guessing**:
      - The sentence is displayed with all words masked.
      - The current word to be guessed is highlighted.
      - `fzf` is launched, allowing you to search the master word list.
      - If your selection is correct, the word is revealed, and you move to the next word.
      - If your selection is incorrect, the game ends, and the correct sentence is shown.
5.  **Victory**: If you guess all the words correctly, you win the round\!

### Clitic Highlighting

If the word ends in a common Finnish clitic, like *-kin* or *-ko*, it will appear in a different color. This system is pretty dumb but I find it to be helpful so that I don't get distracted from figuring out the base word.

## Contributing

Contributions are welcome\! If you have ideas for new features, bug fixes, or improvements, feel free to open an issue or submit a pull request.

## License

See `LICENSE`.
