data "yandex_compute_image" "ubuntu" {
  family = "ubuntu-2404-lts"
}

resource "tls_private_key" "ff" {
  algorithm = "ED25519"
}

resource "local_file" "ssh_private" {
  content         = tls_private_key.ff.private_key_openssh
  filename        = "${path.module}/../../.secrets/ff_yc_ed25519"
  file_permission = "0600"
}

resource "local_file" "ssh_public" {
  content         = tls_private_key.ff.public_key_openssh
  filename        = "${path.module}/../../.secrets/ff_yc_ed25519.pub"
  file_permission = "0644"
}

# Optional dedicated network when subnet_id is empty
resource "yandex_vpc_network" "ff" {
  count = var.subnet_id == "" ? 1 : 0
  name  = "ff-cfg-net"
}

resource "yandex_vpc_subnet" "ff" {
  count          = var.subnet_id == "" ? 1 : 0
  name           = "ff-cfg-subnet"
  zone           = var.zone
  network_id     = yandex_vpc_network.ff[0].id
  v4_cidr_blocks = ["10.20.30.0/24"]
}

locals {
  subnet_id  = var.subnet_id != "" ? var.subnet_id : yandex_vpc_subnet.ff[0].id
  network_id = var.subnet_id != "" ? data.yandex_vpc_subnet.existing[0].network_id : yandex_vpc_network.ff[0].id
}

data "yandex_vpc_network" "ff_net" {
  network_id = local.network_id
}

resource "yandex_vpc_security_group" "ff" {
  name       = "ff-cfg-sg"
  network_id = local.network_id

  ingress {
    protocol       = "TCP"
    description    = "SSH"
    port           = 22
    v4_cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    protocol       = "ANY"
    v4_cidr_blocks = ["0.0.0.0/0"]
  }
}

data "yandex_vpc_subnet" "existing" {
  count     = var.subnet_id != "" ? 1 : 0
  subnet_id = var.subnet_id
}

locals {
  hosts = {
    "host-intel-ht" = {
      platform        = var.platform_intel
      cores           = var.cores
      memory          = var.memory_gb
      core_fraction   = var.core_fraction
      preemptible     = true
      cpu_vendor      = "intel"
      hyper_threading = true
      ff_os_cores     = [0]
      ff_isolated     = [1]
      role_label      = "ping_pong"
    }
    # Своя конфигурация, как в консоли: AMD Zen 4, 2 vCPU, 20%, 1 ГБ, не прерываемая.
    # На standard-v4a при 20% минимум — 2 vCPU и 1 ГБ.
    "host-amd-noht" = {
      platform        = var.platform_amd
      cores           = 2
      memory          = 1
      core_fraction   = 20
      preemptible     = false
      cpu_vendor      = "amd"
      hyper_threading = false
      ff_os_cores     = [0]
      ff_isolated     = [1]
      role_label      = "ping_pong"
    }
  }
}

resource "yandex_compute_instance" "hosts" {
  for_each = local.hosts

  name        = each.key
  hostname    = each.key
  platform_id = each.value.platform
  zone        = var.zone

  resources {
    cores         = each.value.cores
    memory        = each.value.memory
    core_fraction = each.value.core_fraction
  }

  boot_disk {
    initialize_params {
      image_id = data.yandex_compute_image.ubuntu.id
      size     = var.disk_gb
      type     = "network-ssd" # HDD quota in ru-central1-a often exhausted
    }
  }

  network_interface {
    subnet_id = local.subnet_id
    nat       = true
    # custom SG + network default (ANY ingress) — иначе SSH снаружи иногда режется
    security_group_ids = [
      yandex_vpc_security_group.ff.id,
      data.yandex_vpc_network.ff_net.default_security_group_id,
    ]
  }

  metadata = {
    # deploy key (ansible) + your keys from YC / laptop
    ssh-keys = join("\n", concat(
      ["${var.ssh_user}:${chomp(tls_private_key.ff.public_key_openssh)}"],
      [for k in var.ssh_public_keys : "${var.ssh_user}:${chomp(k)}"]
    ))
    user-data = <<-EOT
      #cloud-config
      package_update: true
      packages: [python3]
    EOT
  }

  labels = {
    project         = "ff-cfg"
    cpu_vendor      = each.value.cpu_vendor
    hyper_threading = each.value.hyper_threading ? "true" : "false"
    ff_role         = each.value.role_label
  }

  scheduling_policy {
    preemptible = each.value.preemptible
  }
}
