moved {
  from = module.vpc
  to   = module.network
}

module "network" {
  source = "./modules/vpc"
}

resource "aws_instance" "web" {
  subnet_id = module.network.subnet_id
}
