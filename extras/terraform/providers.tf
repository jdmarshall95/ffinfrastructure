provider "yandex" {
  # Auth: export YC_TOKEN=$(yc iam create-token)
  #        export YC_CLOUD_ID=... YC_FOLDER_ID=...
  cloud_id  = var.cloud_id
  folder_id = var.folder_id
  zone      = var.zone
}
