# Copyright IBM Corp. 2024, 2025
# SPDX-License-Identifier: MPL-2.0


// Some resource without a provider since it is a unit test and 
// handling creds is not needed

resource "random_shuffle" "az" {
  input        = ["us-west-1a", "us-west-1c", "us-west-1d", "us-west-1e"]
  result_count = 2
}
