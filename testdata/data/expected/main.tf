data "aws_ami" "debian" {
  most_recent = true
  owners      = ["099720109477"]
}

resource "aws_instance" "web" {
  ami           = data.aws_ami.debian.id
  instance_type = "t3.micro"
}

output "ami_name" {
  value = data.aws_ami.debian.name
}
