resource "aws_instance" "web" {
  ami = "ami-123"
}

output "instance_id" {
  value = aws_instance.web.id
}

output "public_ip" {
  value = aws_instance.web.public_ip
}
