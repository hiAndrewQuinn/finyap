#!/bin/bash

# --- NEW SETUP SECTION ---

echo "============================================================"
echo " Finnish Yap Practice Scenarios"
echo "============================================================"
echo ""

# 1. Ask for the number of loops per scenario
read -p "Enter number of reviews per scenario [10]: " user_loop_count
# Use 10 if the input is empty, otherwise use the user's input.
loop_count=${user_loop_count:-10}
# Basic validation to ensure the input is a positive integer.
if ! [[ "$loop_count" =~ ^[0-9]+$ ]]; then
    echo "Invalid input. Defaulting to 10."
    loop_count=10
fi
echo ""

# 2. Find all TSV files and ask the user which ones to process
all_tsv_files=$(find scenarios/ -name "*.tsv" | sort)
total_available_files=$(echo "$all_tsv_files" | wc -l | xargs) # xargs trims whitespace

# Exit if no .tsv files are found
if [ "$total_available_files" -eq 0 ]; then
    echo "Error: No .tsv files found in the 'scenarios/' directory."
    echo "Please ensure your scenario files are present before running."
    exit 1
fi

echo "Found ${total_available_files} scenarios."
read -p "Process all of them? [Y/n]: " process_all
echo ""

files_to_process=""

# Default to 'y' (process all) if input is empty or 'y'/'Y'
if [[ -z "$process_all" || "$process_all" == "y" || "$process_all" == "Y" ]]; then
    files_to_process="$all_tsv_files"
else
    # Check if fzf is installed before attempting to use it
    if ! command -v fzf &> /dev/null; then
        echo "Error: fzf is not installed. It's required for file selection."
        echo "On macOS, run: brew install fzf"
        echo "On Debian/Ubuntu, run: sudo apt-get install fzf"
        echo "Aborting."
        exit 1
    fi
    echo "Use TAB to select/deselect files, then press Enter to confirm."
    sleep 1 # Give user a moment to read the instructions
    
    # Use fzf for interactive, fuzzy file selection
    files_to_process=$(echo "$all_tsv_files" | fzf --multi --height 50% --border --prompt="Select scenarios> ")
fi

# Exit if no files were selected from the fzf prompt
if [ -z "$files_to_process" ]; then
    echo "No files selected. Exiting."
    exit 0
fi

# --- END OF SETUP SECTION ---


# Count the number of files that will actually be processed
total_tsv_files=$(echo "$files_to_process" | wc -l | xargs)
current_tsv_index=0

# Create check.csv with a header row if it doesn't already exist.
if [ ! -f check.csv ]; then
  echo "File,English,Finnish" >check.csv
fi

# Iterate over the (potentially shuffled) list of selected files
echo "$files_to_process" | shuf | while read -r file; do
  current_tsv_index=$((current_tsv_index + 1))
  
  # Use the user-defined loop count
  for ((i=1; i<=loop_count; i++)); do
    clear

    # Update progress display with the correct total and loop count
    echo "practice-scenarios.bash: [${current_tsv_index}/${total_tsv_files}] ${file}"
    echo "practice-scenarios.bash: [${i}/${loop_count}] bash finyap.bash --input $file "

    echo ""
    echo "============================================================"
    echo ""

    # Capture the output of finyap.bash into a variable so we can parse it later.
    script_output=$(bash finyap.bash --input "$file")
    echo "$script_output"

    echo ""
    echo "============================================================"
    echo ""

    # Updated the prompt to include the new 'c' option.
    echo "Review your answer."
    echo ""
    echo "- Press Enter to continue."
    echo "- Enter 'q' to (q)uit. (Alternatives: Q, e, E, anything starting with these.)"
    echo "- Enter 'c' to save a sentence to (c)heck later, then continue."
    echo ""
    read -p "$ " user_input </dev/tty

    if [[ -z "$user_input" ]]; then
      continue
    elif [[ "$user_input" == "c" || "$user_input" == "C" ]]; then
      # Use 'grep' to find the lines with the sentences and 'sed' to remove the labels.
      finnish_sentence=$(echo "$script_output" | grep "Finnish:" | sed 's/Finnish: //')
      english_sentence=$(echo "$script_output" | grep "English:" | sed 's/English: //')

      # Append the file path and the extracted sentences as a new line in check.csv.
      # The fields are quoted to prevent issues if a sentence contains a comma.
      echo "\"$file\",\"$english_sentence\",\"$finnish_sentence\"" >>check.csv
      echo "Entry saved to: $(realpath check.csv)"
      echo "Continuing in 1 second."
      sleep 1 # Pause for a moment so you can see the confirmation message.
    elif [[ "$user_input" == "q"* || "$user_input" == "Q"* || "$user_input" == "e"* || "$user_input" == "E"* ]]; then
      echo "Exiting."
      exit 0
    fi
  done
done

echo "All selected scenarios processed."
exit 0
