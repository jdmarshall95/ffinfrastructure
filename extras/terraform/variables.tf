variable "cloud_id" {
  type = string
}

variable "folder_id" {
  type = string
}

variable "zone" {
  type    = string
  default = "ru-central1-a"
}

variable "subnet_id" {
  type        = string
  description = "Existing subnet (e.g. hft-lab-subnet-a). Empty = create dedicated network."
  default     = ""
}

variable "ssh_user" {
  type    = string
  default = "ubuntu"
}

variable "ssh_public_keys" {
  type        = list(string)
  description = "Extra public keys (e.g. your laptop key from YC profile). Always merged with the generated deploy key."
  default     = []
}

variable "cores" {
  type    = number
  default = 2
}

variable "memory_gb" {
  type    = number
  default = 2
}

variable "disk_gb" {
  type        = number
  default     = 8
  description = "Boot disk GB. ubuntu-2204-lts minimum is 8."
}

variable "core_fraction" {
  type    = number
  default = 20 # cheaper burstable VMs
}

variable "platform_intel" {
  type    = string
  default = "standard-v3"
}

variable "platform_amd" {
  type        = string
  default     = "standard-v4a"
  description = "AMD Zen 4 (standard-v4a). В зонах ru-central1-d и ru-central1-e этой платформы нет."
}
