package ipfscore

func (*coreInterface) Remove(_ string) error          { return errReadOnly }
func (*coreInterface) RemoveLink(_ string) error      { return errReadOnly }
func (*coreInterface) RemoveDirectory(_ string) error { return errReadOnly }
