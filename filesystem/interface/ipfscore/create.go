package ipfscore

func (*coreInterface) Make(string) error             { return errReadOnly }
func (*coreInterface) MakeDirectory(string) error    { return errReadOnly }
func (*coreInterface) MakeLink(string, string) error { return errReadOnly }
