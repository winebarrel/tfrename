module "vpc" {
  source = "./modules/vpc"
  cidr   = "10.0.0.0/16"
}

resource "aws_instance" "web" {
  subnet_id = module.vpc.subnet_ids[0]
}

output "vpc_id" {
  value = module.vpc.id
}
