resource "aws_instance" "bar" {
  ami           = "ami-123"
  instance_type = "t3.micro"
}

resource "aws_eip" "addr" {
  instance = aws_instance.bar.id
}

output "ip" {
  value = aws_instance.bar.public_ip
}
