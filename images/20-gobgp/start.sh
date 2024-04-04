#!/bin/bash

# Define the output file
config_file="/etc/gobgp.toml"

# Initialize associative arrays for globals, neighbors, and sub-settings of neighbors
declare -A globals
declare -A neighbors
declare -A neighbor_subsettings

# Function to add quotes to non-numeric values
quote_if_needed() {
  if [[ $1 =~ ^[0-9]+$ ]]; then
    echo $1
  else
    echo "\"$1\""
  fi
}

# Process each environment variable
while IFS='=' read -r name value ; do
  if [[ $name == GOBGP_GLOBAL_* ]]; then
    key=$(echo "$name" | sed -e 's/GOBGP_GLOBAL_//' -e 'y/_/-/')
    globals[$key]=$(quote_if_needed "$value")
  elif [[ $name =~ ^GOBGP_NEIGHBOR[0-9]+_ ]]; then
    neighbor_index=$(echo "$name" | grep -oP 'GOBGP_NEIGHBOR\K[0-9]+')
    key=$(echo "$name" | sed -r 's/GOBGP_NEIGHBOR[0-9]+_//')

    if [[ $key =~ [A-Z]+_ ]]; then
      # This is a sub-setting
      sub_setting=$(echo "$key" | cut -d '_' -f1 | tr '[:upper:]' '[:lower:]')
      key=$(echo "$key" | sed -r "s/${sub_setting}_//" | tr '_' '-' | tr '[:upper:]' '[:lower:]')
      neighbor_subsettings[$neighbor_index,$sub_setting,$key]=$(quote_if_needed "$value")
    else
      # Main neighbor setting
      key=$(echo "$key" | tr '_' '-')
      neighbors[$neighbor_index,$key]=$(quote_if_needed "$value")
    fi
  fi
done < <(env)

# Write the global config
echo "[global.config]" > "$config_file"
for key in "${!globals[@]}"; do
  echo "  $key = ${globals[$key]}" >> "$config_file"
done
echo "" >> "$config_file"

# Write the neighbors config
for key in "${!neighbors[@]}"; do
  IFS=',' read -r neighbor_index key_name <<< "$key"
  # Start the neighbors section only if it's the first setting of this neighbor
  if [[ ! ${visited_neighbors[$neighbor_index]+_} ]]; then
    echo "[[neighbors]]" >> "$config_file"
    echo "  [neighbors.config]" >> "$config_file"
    visited_neighbors[$neighbor_index]=1
  fi
  echo "    $key_name = ${neighbors[$key]}" >> "$config_file"
done

# Write the neighbors' sub-settings
for key in "${!neighbor_subsettings[@]}"; do
  IFS=',' read -r neighbor_index sub_setting key_name <<< "$key"
  if [[ -z ${written_subsettings[$neighbor_index,$sub_setting]+_} ]]; then
    echo "  [neighbors.${sub_setting}.config]" >> "$config_file"
    written_subsettings[$neighbor_index,$sub_setting]=1
  fi
  echo "    $key_name = ${neighbor_subsettings[$key]}" >> "$config_file"
done

gobgpd -f $config_file
