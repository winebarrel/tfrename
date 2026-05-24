resource "zoo_thing" "baz" {
  name = "single"
}

output "id" {
  value = zoo_thing.baz["zoo"].id
}

output "name" {
  value = zoo_thing.baz["zoo"].name
}
