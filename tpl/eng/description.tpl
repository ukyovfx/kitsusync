{{if .IsAssignNotification}}📋 A task has been assigned

{{end}}{{.StatusEmoji}} {{.StatusUpper}}
{{if .IsCommentOnly}}{{.CommentOnlyMessage}}{{else}}{{.StatusMessage}}{{if .StatusTransitionMessage}}
{{.StatusTransitionMessage}}{{end}}{{if .PreviousStatus}}
{{.PreviousStatus}} → {{.StatusUpper}}{{end}}{{end}}{{if .CommentContent}}

{{.CommentLabel}}
{{.CommentContent}}{{if .CommentAuthor}}
— {{.CommentAuthorLabel}}: {{.CommentAuthor}}{{end}}{{end}}{{if .TaskURL}}

{{.LinksLabel}}
[Kitsu]({{.TaskURL}}){{if .GoogleDriveURL}} · [Drive]({{.GoogleDriveURL}}){{end}}{{else if .GoogleDriveURL}}

{{.LinksLabel}}
[Drive]({{.GoogleDriveURL}}){{end}}
