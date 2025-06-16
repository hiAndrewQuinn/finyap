#!/bin/bash

# Find all TSV files and count them
all_tsv_files=$(find scenarios/ -name "*.tsv")
total_tsv_files=$(echo "$all_tsv_files" | wc -l)
current_tsv_index=0

# --- NEW ---
# Create check.csv with a header row if it doesn't already exist.
if [ ! -f check.csv ]; then
  echo "File,English,Finnish" >check.csv
fi

echo "$all_tsv_files" | shuf | while read -r file; do
  current_tsv_index=$((current_tsv_index + 1))
  for i in {1..10}; do
    clear

    echo "practice-scenarios.bash: [${current_tsv_index}/${total_tsv_files}] ${file}"
    echo "practice-scenarios.bash: [${i}/10] bash finyap.bash --input $file "

    echo ""
    echo "============================================================"
    echo ""

    # --- MODIFIED ---
    # Capture the output of finyap.bash into a variable so we can parse it later.
    # 'tee /dev/tty' is used to still print the output to the terminal for you to see.
    script_output=$(bash finyap.bash --input "$file")
    echo "$script_output"

    echo ""
    echo "============================================================"
    echo ""

    # --- MODIFIED ---
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
    # --- NEW 'c' OPTION ---
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

echo "All scenarios processed."
exit 0
