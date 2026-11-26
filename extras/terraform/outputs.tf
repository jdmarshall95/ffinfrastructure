output "hosts" {
  value = {
    for name, inst in yandex_compute_instance.hosts : name => {
      public_ip       = inst.network_interface[0].nat_ip_address
      private_ip      = inst.network_interface[0].ip_address
      cpu_vendor      = local.hosts[name].cpu_vendor
      hyper_threading = local.hosts[name].hyper_threading
      platform        = local.hosts[name].platform
      role            = local.hosts[name].role_label
    }
  }
}

locals {
  pong_hosts = { for name, h in local.hosts : name => h if h.role_label == "pong" }
  ping_hosts = { for name, h in local.hosts : name => h if h.role_label == "ping" }
}

# Inventory только с коннектом. Профиль/сервисы — в ../../nodes/hosts/*.yml
resource "local_file" "ansible_inventory" {
  filename = "${path.module}/../inventory/yc.hosts.yml"
  content = yamlencode({
    all = {
      vars = {
        ansible_user                 = var.ssh_user
        ansible_ssh_private_key_file = abspath("${path.module}/../../.secrets/ff_yc_ed25519")
        ansible_python_interpreter   = "/usr/bin/python3"
        ansible_ssh_common_args      = "-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"
      }
      hosts = {
        for name, inst in yandex_compute_instance.hosts : name => {
          ansible_host  = inst.network_interface[0].nat_ip_address
          ff_private_ip = inst.network_interface[0].ip_address
        }
      }
    }
  })
}
