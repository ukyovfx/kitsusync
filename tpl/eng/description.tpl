{{if .IsAssignNotification}}📋 A task has been assigned

{{end}}{{.StatusEmoji}} {{.StatusUpper}}{{if .EntityType}}{{if .TaskName}}
{{.EntityType}} / {{.TaskName}}{{end}}{{else if .ParentName}}
{{.ParentName}}{{if .TaskName}} / {{.TaskName}}{{end}}{{else if .GroupName}}
{{.GroupName}}{{if .TaskName}} / {{.TaskName}}{{end}}{{else if .TaskName}}
{{.TaskName}}{{end}}
{{if .IsCommentOnly}}{{.CommentOnlyMessage}}{{else}}{{.StatusMessage}}{{if .StatusTransitionMessage}}
{{.StatusTransitionMessage}}{{end}}{{if .PreviousStatus}}
{{.PreviousStatus}} → {{.StatusUpper}}{{end}}{{end}}{{if .CommentContent}}

{{.CommentLabel}}
> {{.CommentContent}}{{if .CommentAuthor}}
— {{.CommentAuthorLabel}}: {{.CommentAuthor}}{{end}}{{end}}{{if .TaskURL}}

🦊 [Kitsu]({{.TaskURL}}){{if .GoogleDriveURL}} · 📁 [Drive]({{.GoogleDriveURL}}){{end}}{{else if .GoogleDriveURL}}
📁 [Drive]({{.GoogleDriveURL}}){{end}}
