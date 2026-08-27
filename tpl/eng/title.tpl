{{- if .EntityType}}{{.EntityType}}{{- else if .ParentName}}{{.ParentName}}{{- else}}{{.GroupName}}{{- end}}{{if .TaskName}} / {{.TaskName}}{{end}}

